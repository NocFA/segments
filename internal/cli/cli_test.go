package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/nocfa/segments/internal/analytics"
	"codeberg.org/nocfa/segments/internal/export"
	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/server"
	"codeberg.org/nocfa/segments/internal/store"
)

func TestCoerceInt(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		def  int
		want int
	}{
		{"float64", float64(2), -1, 2},
		{"int", 3, -1, 3},
		{"int64", int64(4), -1, 4},
		{"json.Number int", json.Number("5"), -1, 5},
		{"string digits", "2", -1, 2},
		{"string padded", "  3 ", -1, 3},
		{"string negative", "-1", 99, -1},
		{"string non-numeric", "hi", 7, 7},
		{"nil", nil, 9, 9},
		{"bool", true, 11, 11},
		{"float with decimal", 2.7, -1, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := coerceInt(c.in, c.def)
			if got != c.want {
				t.Fatalf("coerceInt(%v, %d) = %d, want %d", c.in, c.def, got, c.want)
			}
		})
	}
}

func TestCallToolCreateTaskPriorityAsString(t *testing.T) {
	// Guard: the MCP schema says priority is a "number", but real clients
	// occasionally send it as a string at the top level. callTool must
	// accept both shapes so the priority field is not silently dropped.
	args := map[string]interface{}{
		"priority": "2",
	}
	got := coerceInt(args["priority"], 0)
	if got != 2 {
		t.Fatalf("string priority dropped: got %d, want 2", got)
	}
	args["priority"] = float64(3)
	if got := coerceInt(args["priority"], 0); got != 3 {
		t.Fatalf("number priority dropped: got %d, want 3", got)
	}
}

func TestSelectNextTask_OrderingByPriorityThenAge(t *testing.T) {
	now := time.Now()
	tasks := []models.Task{
		{ID: "a", Title: "P3 old", Status: models.StatusTodo, Priority: 3, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "b", Title: "P1 newer", Status: models.StatusTodo, Priority: 1, CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "c", Title: "P2 oldest", Status: models.StatusTodo, Priority: 2, CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "d", Title: "P0 oldest-overall", Status: models.StatusTodo, Priority: 0, CreatedAt: now.Add(-4 * time.Hour)},
		{ID: "e", Title: "P1 oldest", Status: models.StatusTodo, Priority: 1, CreatedAt: now.Add(-5 * time.Hour)},
	}
	got := selectNextTask(tasks)
	want := []string{"e", "b", "c", "a", "d"}
	if len(got) != len(want) {
		t.Fatalf("length: got %d want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("index %d: got %s (%s), want %s", i, got[i].ID, got[i].Title, w)
		}
	}
}

func TestSelectNextTask_FiltersNonTodoAndBlocked(t *testing.T) {
	tasks := []models.Task{
		{ID: "a", Status: models.StatusInProgress},
		{ID: "b", Status: models.StatusTodo, BlockedBy: "missing"},
		{ID: "c", Status: models.StatusTodo, BlockedBy: "d"},
		{ID: "d", Status: models.StatusDone},
		{ID: "e", Status: models.StatusTodo},
		{ID: "f", Status: models.StatusClosed},
		{ID: "g", Status: models.StatusBlocker},
	}
	got := selectNextTask(tasks)
	found := map[string]bool{}
	for _, g := range got {
		found[g.ID] = true
	}
	if len(got) != 2 || !found["c"] || !found["e"] {
		ids := []string{}
		for _, g := range got {
			ids = append(ids, g.ID)
		}
		t.Fatalf("got %v, want exactly [c e]", ids)
	}
}

func TestMCP_AgentCaptureInEvents(t *testing.T) {
	// Use MkdirTemp (not t.TempDir) because LMDB holds OS file handles on
	// Windows that outlive the test body, which makes t.TempDir cleanup fail.
	dir, err := os.MkdirTemp("", "segments-mcp-analytics-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	eventPath := filepath.Join(dir, "events.jsonl")
	analytics.SetDefault(analytics.NewWriter(eventPath, true))
	defer analytics.SetDefault(nil)

	setMCPAgent("", "")
	defer setMCPAgent("", "")

	st := store.NewStore(dir)
	proj, err := st.CreateProject("analytics-test")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"clientInfo": map[string]interface{}{
				"name":    "test-agent",
				"version": "9.9.9",
			},
		},
	}
	if resp := handleMCP(st, initReq); resp["error"] != nil {
		t.Fatalf("initialize error: %+v", resp)
	}

	callReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(2),
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "segments_create_task",
			"arguments": map[string]interface{}{
				"project_id": proj.ID,
				"title":      "hello from mcp",
				"priority":   float64(2),
			},
		},
	}
	handleMCP(st, callReq)

	events, err := analytics.Read(eventPath)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var agentHit *analytics.Event
	for i := range events {
		e := events[i]
		if e.Source == "mcp" && e.Type == "task:created" && e.Agent != nil && e.Agent.Name == "test-agent" {
			agentHit = &events[i]
			break
		}
	}
	if agentHit == nil {
		t.Fatalf("no MCP event with agent.name=test-agent; got %d events: %+v", len(events), events)
	}
	if agentHit.Agent.Version != "9.9.9" {
		t.Fatalf("agent.version: got %q, want 9.9.9", agentHit.Agent.Version)
	}
	if agentHit.ProjectID != proj.ID {
		t.Fatalf("project_id: got %q, want %q", agentHit.ProjectID, proj.ID)
	}
}

func TestSelectNextTask_EmptyWhenNoCandidates(t *testing.T) {
	tasks := []models.Task{
		{ID: "a", Status: models.StatusInProgress},
		{ID: "b", Status: models.StatusDone},
		{ID: "c", Status: models.StatusTodo, BlockedBy: "a"},
	}
	if got := selectNextTask(tasks); len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestBuildStatsProjectColumn_CountsAndPercent(t *testing.T) {
	out := buildStatsProjectColumn("demo", 4, 1, 5, 2, 12, 33)
	for _, sub := range []string{"DEMO", "4 done", "1 in-flight", "5 ready", "2 blocked", "33%"} {
		if !strings.Contains(out, sub) {
			t.Errorf("missing substring %q in:\n%s", sub, out)
		}
	}
}

func TestBuildStatsAgentsColumn_SurfacesActiveClaim(t *testing.T) {
	now := time.Now()
	events := []analytics.Event{
		{Timestamp: now.Add(-30 * time.Minute), Type: "task:claimed", Source: "mcp",
			Agent: &analytics.Agent{Name: "claude-code"}, ProjectID: "p", TaskID: "task-abc12345-xxxx", ToStatus: "in_progress"},
		{Timestamp: now.Add(-5 * time.Minute), Type: "task:created", Source: "cli",
			ProjectID: "p", TaskID: "task-fresh0000-xxxx"},
	}
	tasks := []models.Task{
		{ID: "task-abc12345-xxxx", Status: models.StatusInProgress},
	}
	out := buildStatsAgentsColumn(events, tasks)
	if !strings.Contains(out, "claude-code") {
		t.Errorf("agents column missing claude-code: %s", out)
	}
	if !strings.Contains(out, "task-abc") {
		t.Errorf("agents column missing active task uuid8: %s", out)
	}
	if !strings.Contains(out, "you") {
		t.Errorf("agents column missing 'you' (cli actor): %s", out)
	}
}

func TestBuildStatsAgentsColumn_EmptyEvents(t *testing.T) {
	out := buildStatsAgentsColumn(nil, nil)
	if !strings.Contains(out, "no agents seen yet") {
		t.Errorf("empty case should show placeholder: %s", out)
	}
}

func TestBuildStatsRecentColumn_NewestFirstAndVerbs(t *testing.T) {
	now := time.Now()
	events := []analytics.Event{
		{Timestamp: now.Add(-1 * time.Hour), Type: "task:created", Source: "cli", TaskID: "aaaa1111bbbb"},
		{Timestamp: now.Add(-10 * time.Minute), Type: "task:completed", Source: "mcp",
			Agent: &analytics.Agent{Name: "claude-code"}, TaskID: "bbbb2222cccc", ToStatus: "done"},
	}
	out := buildStatsRecentColumn(events)
	createdIdx := strings.Index(out, "created")
	completedIdx := strings.Index(out, "completed")
	if createdIdx < 0 || completedIdx < 0 {
		t.Fatalf("missing expected verbs in: %s", out)
	}
	if completedIdx > createdIdx {
		t.Errorf("expected newest (completed) before oldest (created); got completed=%d, created=%d", completedIdx, createdIdx)
	}
	if !strings.Contains(out, "claude-code") {
		t.Errorf("missing agent name claude-code: %s", out)
	}
	if !strings.Contains(out, "aaaa1111") {
		t.Errorf("missing task uuid8 (aaaa1111): %s", out)
	}
}

func TestEventVerb_Mapping(t *testing.T) {
	cases := map[string]string{
		"task:created":   "created",
		"task:claimed":   "picked up",
		"task:completed": "completed",
		"task:closed":    "closed",
		"task:deleted":   "removed",
		"task:updated":   "updated",
	}
	for typ, want := range cases {
		if got := eventVerb(typ); got != want {
			t.Errorf("eventVerb(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestPromptCuesPresent guards the prompt strings against regressions. LLMs
// using the MCP/Pi tools tend to skip priority and blocked_by unless the
// phrasing makes the correct use explicit. If any of these substrings
// disappears, the prompt has likely drifted back toward generic text.
func TestPromptCuesPresent(t *testing.T) {
	for _, cue := range []string{
		"URGENT",
		"NORMAL",
		"BACKLOG",
		"drop everything",
		"blocked_by",
		"correctness",
		"#0",
		"claim",
		"in_progress",
		"segments_update_tasks",
		"segments_delete_tasks",
		"Ready queue",
		"discovered-from",
		"segment it",
	} {
		if !strings.Contains(mcpServerInstructions, cue) {
			t.Errorf("mcpServerInstructions: missing cue %q", cue)
		}
	}
	// The old "at most one in_progress at a time" rule was removed when the
	// claim semantic landed -- it conflicted with bulk-claiming a sequence of
	// tasks. If someone reintroduces it, fail loudly.
	if strings.Contains(mcpServerInstructions, "at most one in_progress") {
		t.Errorf("stale 'at most one in_progress' phrasing is back in the prompt; claim semantic permits multiple in_progress")
	}
}

func TestMCPToolDefsPriorityAndBlockedByCues(t *testing.T) {
	defs := mcpToolDefs()
	want := map[string]bool{
		"segments_create_task":  true,
		"segments_create_tasks": true,
		"segments_update_task":  true,
		"segments_update_tasks": true,
	}
	for _, d := range defs {
		name, _ := d["name"].(string)
		if !want[name] {
			continue
		}
		raw, _ := json.Marshal(d)
		s := string(raw)
		for _, cue := range []string{"URGENT", "NORMAL", "BACKLOG"} {
			if !strings.Contains(s, cue) {
				t.Errorf("%s: priority description missing cue %q", name, cue)
			}
		}
		if name == "segments_create_task" || name == "segments_create_tasks" {
			// create variants must frame blocked_by as REQUIRED when a hard
			// dependency exists; update variants are allowed to be softer
			// since most updates are unrelated to dependency wiring.
			if !strings.Contains(s, "REQUIRED") {
				t.Errorf("%s: blocked_by description missing REQUIRED framing", name)
			}
		}
	}
}

// TestMCPToolDefsAdvertiseBulkVariants ensures the single-task variants tell
// the LLM to prefer the bulk one, and that the bulk variants exist and
// teach the claim semantic. Without this nudge LLMs default to N individual
// calls.
func TestRunExportWritesSnapshotJSONL(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)

	proj, err := s.CreateProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(proj.ID, "first", "body a", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(proj.ID, "second", "body b", 1); err != nil {
		t.Fatal(err)
	}

	outDir, err := os.MkdirTemp("", "segments-cli-out-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)
	outPath := filepath.Join(outDir, "tasks.jsonl")

	if err := runExport(s, []string{"--path", outPath}); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var count, projects, tasks int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var env export.Envelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		count++
		switch env.Event {
		case "project:snapshot":
			projects++
		case "task:snapshot":
			tasks++
		}
	}
	if count != 3 {
		t.Fatalf("lines=%d, want 3", count)
	}
	if projects != 1 || tasks != 2 {
		t.Fatalf("got projects=%d tasks=%d, want 1 and 2", projects, tasks)
	}
}

func TestRunExportDefaultsToCurrentProject(t *testing.T) {
	t.Setenv("SEGMENTS_PROJECT_ID", "")
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)

	other, err := s.CreateProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(other.ID, "other-task", "", 2); err != nil {
		t.Fatal(err)
	}
	demo, err := s.CreateProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(demo.ID, "demo-task", "body", 2); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	workDir := filepath.Join(parent, "demo")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workDir)

	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	if err := runExport(s, []string{"--path", outPath}); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var projectNames []string
	var taskTitles []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var env export.Envelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Project != nil {
			projectNames = append(projectNames, env.Project.Name)
		}
		if env.Task != nil {
			taskTitles = append(taskTitles, env.Task.Title)
		}
	}
	if len(projectNames) != 1 || projectNames[0] != "demo" {
		t.Fatalf("projects = %v, want [demo]", projectNames)
	}
	if len(taskTitles) != 1 || taskTitles[0] != "demo-task" {
		t.Fatalf("tasks = %v, want [demo-task]", taskTitles)
	}
}

func TestRunExportAmbiguousProjectsErrors(t *testing.T) {
	t.Setenv("SEGMENTS_PROJECT_ID", "")
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)
	if _, err := s.CreateProject("foo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("bar"); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	workDir := filepath.Join(parent, "nomatch")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workDir)

	err = runExport(s, []string{"--path", filepath.Join(t.TempDir(), "x.jsonl")})
	if err == nil {
		t.Fatal("expected error for ambiguous resolution")
	}
	if !strings.Contains(err.Error(), "cannot auto-resolve") {
		t.Errorf("expected 'cannot auto-resolve' error, got: %v", err)
	}
}

func TestRunExportAllWritesToDataDir(t *testing.T) {
	t.Setenv("SEGMENTS_PROJECT_ID", "")
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)
	if _, err := s.CreateProject("one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("two"); err != nil {
		t.Fatal(err)
	}

	origDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = origDataDir })

	if err := runExport(s, []string{"--all"}); err != nil {
		t.Fatalf("runExport --all: %v", err)
	}

	wantPath := filepath.Join(dataDir, "tasks.jsonl")
	f, err := os.Open(wantPath)
	if err != nil {
		t.Fatalf("expected export at %s: %v", wantPath, err)
	}
	defer f.Close()

	names := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var env export.Envelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Project != nil {
			names[env.Project.Name] = true
		}
	}
	if !names["one"] || !names["two"] {
		t.Errorf("both projects should be exported, got: %v", names)
	}
}

func TestRunExportPathOverridesAllDefault(t *testing.T) {
	t.Setenv("SEGMENTS_PROJECT_ID", "")
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)
	if _, err := s.CreateProject("one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("two"); err != nil {
		t.Fatal(err)
	}

	origDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = origDataDir })

	outPath := filepath.Join(t.TempDir(), "custom.jsonl")
	if err := runExport(s, []string{"--all", "--path", outPath}); err != nil {
		t.Fatalf("runExport: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected file at --path %s: %v", outPath, err)
	}
	homeDefault := filepath.Join(dataDir, "tasks.jsonl")
	if _, err := os.Stat(homeDefault); err == nil {
		t.Errorf("home default %s should not exist when --path is given", homeDefault)
	}
}

func TestRunExportAllAndProjectRejected(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "segments-cli-export-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)
	s := store.NewStore(baseDir)
	if _, err := s.CreateProject("demo"); err != nil {
		t.Fatal(err)
	}

	err = runExport(s, []string{"--all", "--project", "demo"})
	if err == nil {
		t.Fatal("expected error for --all + --project combo")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error, got: %v", err)
	}
}

func TestResolveTaskRef_HintPIDMatches(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-1-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)

	proj, _ := st.CreateProject("alpha")
	task, _ := st.CreateTask(proj.ID, "t1", "body", 2)

	ref, err := resolveTaskRef(st, proj.ID, task.ID)
	if err != nil {
		t.Fatalf("resolveTaskRef err: %v", err)
	}
	if ref.Task.ID != task.ID || ref.ProjectID != proj.ID || ref.ResolvedFromOverride {
		t.Errorf("unexpected ref: %+v", ref)
	}
}

func TestResolveTaskRef_HintPIDOverridden(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-2-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)

	p1, _ := st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")
	task, _ := st.CreateTask(p2.ID, "lives in beta", "", 2)

	// Caller passes alpha as hint, but task lives in beta.
	ref, err := resolveTaskRef(st, p1.ID, task.ID)
	if err != nil {
		t.Fatalf("resolveTaskRef err: %v", err)
	}
	if ref.ProjectID != p2.ID {
		t.Errorf("ProjectID = %q, want %q (beta)", ref.ProjectID, p2.ID)
	}
	if !ref.ResolvedFromOverride {
		t.Errorf("expected ResolvedFromOverride=true when hint pid differed from actual")
	}
}

func TestResolveTaskRef_NoHintScansAllProjects(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-3-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)

	_, _ = st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")
	task, _ := st.CreateTask(p2.ID, "findable", "", 2)

	ref, err := resolveTaskRef(st, "", task.ID)
	if err != nil {
		t.Fatalf("resolveTaskRef err: %v", err)
	}
	if ref.ProjectID != p2.ID {
		t.Errorf("ProjectID = %q, want %q", ref.ProjectID, p2.ID)
	}
	if ref.ResolvedFromOverride {
		t.Errorf("expected ResolvedFromOverride=false when no hint was passed")
	}
}

func TestResolveTaskRef_PrefixMatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-4-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)

	proj, _ := st.CreateProject("alpha")
	task, _ := st.CreateTask(proj.ID, "t", "", 2)

	ref, err := resolveTaskRef(st, "", task.ID[:8])
	if err != nil {
		t.Fatalf("resolveTaskRef err: %v", err)
	}
	if ref.Task.ID != task.ID {
		t.Errorf("got %q, want %q", ref.Task.ID, task.ID)
	}
}

func TestResolveTaskRef_NotFoundWrapsLMDB(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-5-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)
	_, _ = st.CreateProject("alpha")

	_, err = resolveTaskRef(st, "", "missing-task-id")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "MDB_NOTFOUND") || strings.Contains(err.Error(), "mdb_get") {
		t.Errorf("error leaked raw LMDB text: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected wrapped 'not found' error, got: %v", err)
	}
}

func TestResolveTaskRef_AmbiguousPrefixReturnsStructuredErr(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ref-6-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st := store.NewStore(dir)

	p1, _ := st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")

	// Create many tasks to raise the chance of a shared short prefix.
	for i := 0; i < 10; i++ {
		st.CreateTask(p1.ID, "a", "", 2)
		st.CreateTask(p2.ID, "b", "", 2)
	}

	// Find a 1-char hex prefix that matches at least 2 tasks across projects.
	var shared string
	for _, hex := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"} {
		matches, _ := st.FindTaskAny(hex)
		if len(matches) >= 2 {
			shared = hex
			break
		}
	}
	if shared == "" {
		t.Skip("no shared hex prefix in this random run")
	}

	_, err = resolveTaskRef(st, "", shared)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	var ae *ambiguousTaskErr
	if !errors.As(err, &ae) {
		t.Fatalf("expected *ambiguousTaskErr, got %T: %v", err, err)
	}
	if len(ae.Candidates) < 2 {
		t.Errorf("expected >=2 candidates, got %d", len(ae.Candidates))
	}

	out, ok := emitAmbigTaskErr(st, err)
	if !ok {
		t.Fatal("emitAmbigTaskErr should recognize *ambiguousTaskErr")
	}
	if !strings.Contains(out, "ambiguous task id") || !strings.Contains(out, "candidates") {
		t.Errorf("JSON output missing expected fields: %s", out)
	}
}

func TestMCP_GetTaskCrossProject(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-mcp-get-xp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p1, _ := st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")
	task, _ := st.CreateTask(p2.ID, "in beta", "a body", 2)

	// Caller passes alpha as hint but the task lives in beta. With the fix,
	// get_task still finds it and surfaces resolved_from_project_id.
	args := map[string]interface{}{"project_id": p1.ID, "task_id": task.ID}
	out := callTool(st, "segments_get_task", args)
	if strings.Contains(out, "MDB_NOTFOUND") {
		t.Fatalf("LMDB error leaked: %s", out)
	}
	var parsed map[string]interface{}
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		t.Fatalf("json parse: %v; raw: %s", jerr, out)
	}
	if parsed["id"] != task.ID {
		t.Errorf("id = %v, want %s", parsed["id"], task.ID)
	}
	if parsed["resolved_from_project_id"] != p2.ID {
		t.Errorf("resolved_from_project_id = %v, want %s", parsed["resolved_from_project_id"], p2.ID)
	}

	// Same call without project_id should also work (scan-all path), and must
	// NOT include resolved_from_project_id (no hint was given to override).
	args = map[string]interface{}{"task_id": task.ID}
	out = callTool(st, "segments_get_task", args)
	var parsed2 map[string]interface{}
	if jerr := json.Unmarshal([]byte(out), &parsed2); jerr != nil {
		t.Fatalf("json parse: %v; raw: %s", jerr, out)
	}
	if parsed2["id"] != task.ID {
		t.Errorf("id = %v, want %s", parsed2["id"], task.ID)
	}
	if _, present := parsed2["resolved_from_project_id"]; present {
		t.Errorf("resolved_from_project_id should be omitted when no hint was given, got: %s", out)
	}
}

func TestMCP_UpdateTaskCrossProject(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-mcp-upd-xp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p1, _ := st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")
	task, _ := st.CreateTask(p2.ID, "in beta", "", 2)

	args := map[string]interface{}{
		"project_id": p1.ID,
		"task_id":    task.ID,
		"status":     "in_progress",
	}
	out := callTool(st, "segments_update_task", args)
	if strings.Contains(out, "MDB_NOTFOUND") {
		t.Fatalf("LMDB error leaked: %s", out)
	}
	var parsed map[string]interface{}
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		t.Fatalf("json parse: %v; raw: %s", jerr, out)
	}
	if parsed["status"] != "in_progress" {
		t.Errorf("status = %v, want in_progress", parsed["status"])
	}
	if parsed["resolved_from_project_id"] != p2.ID {
		t.Errorf("resolved_from_project_id missing: %s", out)
	}
	// Verify the real update landed in p2 (not p1).
	stored, _ := st.GetTask(p2.ID, task.ID)
	if stored.Status != models.StatusInProgress {
		t.Errorf("stored status = %q, want in_progress", stored.Status)
	}
}

func TestMCP_DeleteTaskCrossProject(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-mcp-del-xp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p1, _ := st.CreateProject("alpha")
	p2, _ := st.CreateProject("beta")
	task, _ := st.CreateTask(p2.ID, "to delete", "", 2)

	args := map[string]interface{}{"project_id": p1.ID, "task_id": task.ID}
	out := callTool(st, "segments_delete_task", args)
	if !strings.Contains(out, `"deleted":true`) {
		t.Fatalf("unexpected delete response: %s", out)
	}
	if !strings.Contains(out, "resolved_from_project_id") {
		t.Errorf("resolved_from_project_id missing: %s", out)
	}
	if _, err := st.GetTask(p2.ID, task.ID); err == nil {
		t.Errorf("task still exists after delete")
	}
}

func TestMCP_GetTaskMissingTask_WrappedError(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-mcp-miss-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	st.CreateProject("alpha")

	out := callTool(st, "segments_get_task", map[string]interface{}{"task_id": "nonexistent"})
	if strings.Contains(out, "MDB_NOTFOUND") || strings.Contains(out, "mdb_get") {
		t.Errorf("raw LMDB error leaked: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected wrapped not-found error: %s", out)
	}
}

func TestMCPToolDefsAdvertiseBulkVariants(t *testing.T) {
	defs := mcpToolDefs()
	byName := map[string]string{}
	for _, d := range defs {
		name, _ := d["name"].(string)
		raw, _ := json.Marshal(d)
		byName[name] = string(raw)
	}
	for _, name := range []string{"segments_create_tasks", "segments_update_tasks", "segments_delete_tasks"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing bulk tool: %s", name)
		}
	}
	for _, pair := range []struct{ single, bulk string }{
		{"segments_create_task", "segments_create_tasks"},
		{"segments_update_task", "segments_update_tasks"},
		{"segments_delete_task", "segments_delete_tasks"},
	} {
		desc := byName[pair.single]
		if !strings.Contains(desc, pair.bulk) {
			t.Errorf("%s description does not point at bulk variant %s", pair.single, pair.bulk)
		}
	}
	// segments_update_tasks description must teach the claim-sequence idiom
	// (the whole reason the tool exists), so regressions are caught.
	if desc := byName["segments_update_tasks"]; !strings.Contains(desc, "claim") || !strings.Contains(desc, "in_progress") {
		t.Errorf("segments_update_tasks description missing claim / in_progress framing: %s", desc)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
		err  bool
	}{
		{"", time.Time{}, false},
		{"7d", now.Add(-7 * 24 * time.Hour), false},
		{"1d", now.Add(-24 * time.Hour), false},
		{"24h", now.Add(-24 * time.Hour), false},
		{"30m", now.Add(-30 * time.Minute), false},
		{"90s", now.Add(-90 * time.Second), false},
		{"2026-04-20T00:00:00Z", time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), false},
		{"garbage", time.Time{}, true},
		{"-5d", time.Time{}, true},
	}
	for _, c := range cases {
		got, err := parseSince(now, c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseSince(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q): unexpected error %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseSince(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMCPListTasks_CompactStripsBody(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-compact-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	st.CreateTask(p.ID, "t1", "BODY-CONTENT-MARKER", 2)

	out := callTool(st, "segments_list_tasks", map[string]interface{}{"project_id": p.ID})
	if strings.Contains(out, "BODY-CONTENT-MARKER") {
		t.Errorf("compact default leaked body: %s", out)
	}
	if strings.Contains(out, `"body"`) {
		t.Errorf("compact response includes body field: %s", out)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected bare array: %v, raw: %s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 task, got %d", len(got))
	}
	for _, field := range []string{"id", "title", "status", "priority", "updated_at"} {
		if _, ok := got[0][field]; !ok {
			t.Errorf("compact row missing %s: %v", field, got[0])
		}
	}
}

func TestMCPListTasks_FieldsFullKeepsBody(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-full-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	st.CreateTask(p.ID, "t1", "BODY-CONTENT-MARKER", 2)

	out := callTool(st, "segments_list_tasks", map[string]interface{}{
		"project_id": p.ID,
		"fields":     "full",
	})
	if !strings.Contains(out, "BODY-CONTENT-MARKER") {
		t.Errorf("fields=full dropped body: %s", out)
	}
}

func TestMCPListTasks_LimitCaps(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-limit-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	for i := 0; i < 5; i++ {
		st.CreateTask(p.ID, "t", "", 2)
	}

	out := callTool(st, "segments_list_tasks", map[string]interface{}{
		"project_id": p.ID,
		"limit":      float64(2),
	})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 2 {
		t.Errorf("limit=2 returned %d rows", len(got))
	}
}

func TestMCPListTasks_SinceFiltersByUpdatedAt(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-since-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	old, _ := st.CreateTask(p.ID, "old", "", 2)
	time.Sleep(120 * time.Millisecond)
	fresh, _ := st.CreateTask(p.ID, "fresh", "", 2)

	out := callTool(st, "segments_list_tasks", map[string]interface{}{
		"project_id": p.ID,
		"since":      "100ms",
	})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("since=100ms returned %d tasks, want 1 (old=%s fresh=%s): %s", len(got), old.ID, fresh.ID, out)
	}
	if got[0]["id"] != fresh.ID {
		t.Errorf("since filter kept wrong task: %v (want %s)", got[0]["id"], fresh.ID)
	}
}

func TestMCPListTasks_OrderByClosedAtDesc(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-order-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	a, _ := st.CreateTask(p.ID, "a", "", 2)
	b, _ := st.CreateTask(p.ID, "b", "", 2)
	c, _ := st.CreateTask(p.ID, "c", "", 2)

	// Close in order a, c, b. Expected closed_at_desc order: b, c, a.
	st.UpdateTask(p.ID, a.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(20 * time.Millisecond)
	st.UpdateTask(p.ID, c.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(20 * time.Millisecond)
	st.UpdateTask(p.ID, b.ID, "", "", models.StatusDone, -1, "")

	// status=done should auto-pick closed_at_desc when order_by unset.
	out := callTool(st, "segments_list_tasks", map[string]interface{}{
		"project_id": p.ID,
		"status":     "done",
	})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %s", len(got), out)
	}
	wantOrder := []string{b.ID, c.ID, a.ID}
	for i, want := range wantOrder {
		if got[i]["id"] != want {
			t.Errorf("row %d: got id=%v, want %s; full=%s", i, got[i]["id"], want, out)
		}
	}
}

func TestMCPListTasks_TruncationWrapsResponse(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-trunc-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	// 20 tasks x ~4KB body each = ~80KB, well over the 50KB cap when fields=full.
	big := strings.Repeat("x", 4096)
	for i := 0; i < 20; i++ {
		st.CreateTask(p.ID, "t", big, 2)
	}

	out := callTool(st, "segments_list_tasks", map[string]interface{}{
		"project_id": p.ID,
		"fields":     "full",
		"limit":      float64(1000),
	})
	var wrap map[string]interface{}
	if err := json.Unmarshal([]byte(out), &wrap); err != nil {
		t.Fatalf("expected wrapper object, parse failed: %v, raw head: %s", err, out[:min(200, len(out))])
	}
	if tr, _ := wrap["truncated"].(bool); !tr {
		t.Fatalf("truncated flag missing: %s", out[:min(400, len(out))])
	}
	total, _ := wrap["total"].(float64)
	if int(total) != 20 {
		t.Errorf("total = %v, want 20", total)
	}
	tasks, _ := wrap["tasks"].([]interface{})
	if len(tasks) >= 20 {
		t.Errorf("truncation kept all rows: returned=%d", len(tasks))
	}
	if len(out) > listTasksMaxBytes+2048 {
		t.Errorf("truncated payload still oversized: %d bytes", len(out))
	}
	if hint, _ := wrap["hint"].(string); hint == "" {
		t.Errorf("hint missing from truncated response")
	}
}

func TestMCPListTasksSchemaAdvertisesNewParams(t *testing.T) {
	defs := mcpToolDefs()
	var schemaRaw string
	for _, d := range defs {
		if name, _ := d["name"].(string); name == "segments_list_tasks" {
			raw, _ := json.Marshal(d)
			schemaRaw = string(raw)
		}
	}
	if schemaRaw == "" {
		t.Fatal("segments_list_tasks missing from tool defs")
	}
	for _, param := range []string{"limit", "since", "fields", "order_by"} {
		if !strings.Contains(schemaRaw, `"`+param+`"`) {
			t.Errorf("segments_list_tasks schema missing %s: %s", param, schemaRaw)
		}
	}
}

func TestMCPRecent_OrdersByClosedAtDesc(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-order-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	a, _ := st.CreateTask(p.ID, "a", "first line of a\nsecond", 2)
	b, _ := st.CreateTask(p.ID, "b", "first line of b", 2)
	c, _ := st.CreateTask(p.ID, "c", "first line of c", 2)

	st.UpdateTask(p.ID, a.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(20 * time.Millisecond)
	st.UpdateTask(p.ID, c.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(20 * time.Millisecond)
	st.UpdateTask(p.ID, b.ID, "", "", models.StatusClosed, -1, "")

	out := callTool(st, "segments_recent", map[string]interface{}{"project_id": p.ID})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected bare array, got error %v: %s", err, out)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %s", len(got), out)
	}
	wantOrder := []string{b.ID, c.ID, a.ID}
	for i, want := range wantOrder {
		if got[i]["id"] != want {
			t.Errorf("row %d: got id=%v, want %s; full=%s", i, got[i]["id"], want, out)
		}
	}
	if got[0]["summary"] != "first line of b" {
		t.Errorf("summary not derived from body first line: %v", got[0]["summary"])
	}
	if _, ok := got[0]["body"]; ok {
		t.Errorf("recent row leaked body field: %v", got[0])
	}
	for _, row := range got {
		if row["project_id"] != p.ID {
			t.Errorf("missing project_id on row %v", row)
		}
		if row["project_name"] != "alpha" {
			t.Errorf("missing project_name on row %v", row)
		}
	}
}

func TestMCPRecent_LimitDefaultAndOverride(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-limit-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	for i := 0; i < 15; i++ {
		tk, _ := st.CreateTask(p.ID, "t", "body", 2)
		st.UpdateTask(p.ID, tk.ID, "", "", models.StatusDone, -1, "")
	}

	out := callTool(st, "segments_recent", map[string]interface{}{"project_id": p.ID})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse default: %v, raw: %s", err, out)
	}
	if len(got) != 10 {
		t.Errorf("default limit: got %d rows, want 10", len(got))
	}

	out = callTool(st, "segments_recent", map[string]interface{}{
		"project_id": p.ID,
		"limit":      float64(3),
	})
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse limit=3: %v, raw: %s", err, out)
	}
	if len(got) != 3 {
		t.Errorf("limit=3: got %d rows", len(got))
	}
}

func TestMCPRecent_SinceExcludesOld(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-since-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	old, _ := st.CreateTask(p.ID, "old", "", 2)
	st.UpdateTask(p.ID, old.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(150 * time.Millisecond)
	fresh, _ := st.CreateTask(p.ID, "fresh", "", 2)
	st.UpdateTask(p.ID, fresh.ID, "", "", models.StatusDone, -1, "")

	out := callTool(st, "segments_recent", map[string]interface{}{
		"project_id": p.ID,
		"since":      "100ms",
	})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("since=100ms returned %d rows, want 1: %s", len(got), out)
	}
	if got[0]["id"] != fresh.ID {
		t.Errorf("since kept wrong task: %v (want %s)", got[0]["id"], fresh.ID)
	}
}

func TestMCPRecent_SkipsOpenTasks(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-open-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	st.CreateTask(p.ID, "still-todo", "", 2)
	inProg, _ := st.CreateTask(p.ID, "working", "", 2)
	st.UpdateTask(p.ID, inProg.ID, "", "", models.StatusInProgress, -1, "")
	done, _ := st.CreateTask(p.ID, "done-one", "", 2)
	st.UpdateTask(p.ID, done.ID, "", "", models.StatusDone, -1, "")

	out := callTool(st, "segments_recent", map[string]interface{}{"project_id": p.ID})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 closed row, got %d: %s", len(got), out)
	}
	if got[0]["id"] != done.ID {
		t.Errorf("returned wrong task id: %v", got[0]["id"])
	}
}

func TestMCPRecent_CombinesAcrossProjectsWhenIDOmitted(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-multi-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	pa, _ := st.CreateProject("alpha")
	pb, _ := st.CreateProject("beta")
	ta, _ := st.CreateTask(pa.ID, "alpha-task", "alpha body", 2)
	tb, _ := st.CreateTask(pb.ID, "beta-task", "beta body", 2)
	st.UpdateTask(pa.ID, ta.ID, "", "", models.StatusDone, -1, "")
	time.Sleep(20 * time.Millisecond)
	st.UpdateTask(pb.ID, tb.ID, "", "", models.StatusDone, -1, "")

	out := callTool(st, "segments_recent", map[string]interface{}{})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows across projects, got %d: %s", len(got), out)
	}
	if got[0]["id"] != tb.ID {
		t.Errorf("expected most recent (beta) first, got %v", got[0])
	}
	names := map[string]bool{}
	for _, row := range got {
		names[row["project_name"].(string)] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("missing project_name across rows: %v", names)
	}
}

func TestMCPRecent_ScopedToProjectExcludesOthers(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-recent-scope-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	pa, _ := st.CreateProject("alpha")
	pb, _ := st.CreateProject("beta")
	ta, _ := st.CreateTask(pa.ID, "alpha-task", "", 2)
	tb, _ := st.CreateTask(pb.ID, "beta-task", "", 2)
	st.UpdateTask(pa.ID, ta.ID, "", "", models.StatusDone, -1, "")
	st.UpdateTask(pb.ID, tb.ID, "", "", models.StatusDone, -1, "")

	out := callTool(st, "segments_recent", map[string]interface{}{"project_id": pa.ID})
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse: %v, raw: %s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row scoped to alpha, got %d: %s", len(got), out)
	}
	if got[0]["id"] != ta.ID {
		t.Errorf("wrong task: %v", got[0]["id"])
	}
}

func TestMCPRecent_SchemaAppears(t *testing.T) {
	defs := mcpToolDefs()
	found := false
	for _, d := range defs {
		if name, _ := d["name"].(string); name == "segments_recent" {
			found = true
			raw, _ := json.Marshal(d)
			for _, param := range []string{"limit", "since", "project_id"} {
				if !strings.Contains(string(raw), `"`+param+`"`) {
					t.Errorf("segments_recent schema missing %s: %s", param, raw)
				}
			}
		}
	}
	if !found {
		t.Fatal("segments_recent missing from tool defs")
	}
}

func TestSummaryFromBody(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"\n\n  \n", ""},
		{"first\nsecond", "first"},
		{"\n\n  hello  \nworld", "hello"},
		{strings.Repeat("x", 200), strings.Repeat("x", 120)},
	}
	for _, c := range cases {
		got := summaryFromBody(c.in)
		if got != c.want {
			t.Errorf("summaryFromBody(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRelativeAgo(t *testing.T) {
	if got := relativeAgo(time.Time{}); got != "" {
		t.Errorf("zero time: got %q, want empty", got)
	}
	now := time.Now()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"30s", now.Add(-30 * time.Second), "<1m ago"},
		{"5m", now.Add(-5 * time.Minute), "5m ago"},
		{"3h", now.Add(-3 * time.Hour), "3h ago"},
		{"5d", now.Add(-5 * 24 * time.Hour), "5d ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relativeAgo(c.t); got != c.want {
				t.Errorf("relativeAgo(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestFilterRecentEntries(t *testing.T) {
	now := time.Now()
	t1 := now.Add(-5 * time.Minute)
	t2 := now.Add(-2 * time.Hour)
	t3 := now.Add(-48 * time.Hour)

	mk := func(id string, status models.TaskStatus, closedAt time.Time, projID, projName string) recentEntry {
		var ca *time.Time
		if !closedAt.IsZero() {
			c := closedAt
			ca = &c
		}
		return recentEntry{
			Task:        models.Task{ID: id, Status: status, ClosedAt: ca, UpdatedAt: closedAt},
			ProjectID:   projID,
			ProjectName: projName,
		}
	}

	open := mk("open", models.StatusTodo, t1, "p1", "alpha")
	a := mk("a", models.StatusDone, t1, "p1", "alpha")
	b := mk("b", models.StatusClosed, t2, "p1", "alpha")
	c := mk("c", models.StatusDone, t3, "p1", "alpha")

	got := filterRecentEntries([]recentEntry{open, c, a, b}, time.Time{}, 0)
	if len(got) != 3 {
		t.Fatalf("dropped open task: want 3, got %d", len(got))
	}
	wantIDs := []string{"a", "b", "c"}
	for i, w := range wantIDs {
		if got[i].Task.ID != w {
			t.Errorf("order pos %d: got %s, want %s", i, got[i].Task.ID, w)
		}
	}

	cutoff := now.Add(-30 * time.Minute)
	got = filterRecentEntries([]recentEntry{open, c, a, b}, cutoff, 0)
	if len(got) != 1 || got[0].Task.ID != "a" {
		t.Errorf("since cutoff: got %v", got)
	}

	got = filterRecentEntries([]recentEntry{open, c, a, b}, time.Time{}, 2)
	if len(got) != 2 {
		t.Errorf("limit=2: got %d rows", len(got))
	}
	if got[0].Task.ID != "a" || got[1].Task.ID != "b" {
		t.Errorf("limit kept wrong rows: %v", got)
	}
}

func TestRunRecent_DispatchAlias(t *testing.T) {
	for _, name := range []string{"recent"} {
		found := false
		for _, g := range cmdGroups {
			for _, c := range g.cmds {
				if c.name == name {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("cmdGroups missing %q", name)
		}
	}
}

func TestRunList_NoRecentFlagAccepted(t *testing.T) {
	dir, _ := os.MkdirTemp("", "segments-list-norecent-")
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	tk, _ := st.CreateTask(p.ID, "t", "", 2)
	st.UpdateTask(p.ID, tk.ID, "", "", models.StatusDone, -1, "")

	if err := runList(st, []string{p.ID, "--no-recent"}); err != nil {
		t.Fatalf("runList --no-recent failed: %v", err)
	}
	if err := runList(st, []string{"--no-recent"}); err != nil {
		t.Fatalf("runList projects --no-recent failed: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildContextPayload_SegmentsContextBlock verifies the SessionStart
// banner emits the CWD-resolved project, its in-progress tasks, and its
// recently-closed tasks with relative age. Cross-project dumps are intentionally
// NOT included -- the agent calls segments_list_projects / segments_recent
// on demand instead.
func TestBuildContextPayload_SegmentsContextBlock(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ctx-block-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	origWD, _ := os.Getwd()
	cwd := filepath.Join(dir, "alpha")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWD)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	beta, _ := st.CreateProject("beta")
	ip, _ := st.CreateTask(p.ID, "active work", "", 2)
	st.UpdateTask(p.ID, ip.ID, "", "", models.StatusInProgress, -1, "")
	closed, _ := st.CreateTask(p.ID, "shipped yesterday", "body text", 2)
	st.UpdateTask(p.ID, closed.ID, "", "", models.StatusDone, -1, "")
	// A task in the other project must NOT appear in the output.
	other, _ := st.CreateTask(beta.ID, "beta ready task", "", 2)

	out := buildContextPayload(st, &server.Config{})
	if !strings.Contains(out, "# segmentsContext") {
		t.Fatalf("segmentsContext block missing:\n%s", out)
	}
	if !strings.Contains(out, "Project: alpha  project_id="+p.ID) {
		t.Errorf("expected CWD-resolved project alpha, got:\n%s", out)
	}
	if !strings.Contains(out, "In-progress (up to 5):") {
		t.Errorf("missing in-progress header:\n%s", out)
	}
	if !strings.Contains(out, ip.ID[:8]+"  active work") {
		t.Errorf("in-progress row missing:\n%s", out)
	}
	if !strings.Contains(out, "Recently closed (last 5):") {
		t.Errorf("missing recent-closed header:\n%s", out)
	}
	if !strings.Contains(out, closed.ID[:8]+"  shipped yesterday (") {
		t.Errorf("recent-closed row missing:\n%s", out)
	}
	// Other projects must not leak into the banner.
	if strings.Contains(out, "beta") || strings.Contains(out, other.ID[:8]) {
		t.Errorf("cross-project content leaked into banner:\n%s", out)
	}
}

// TestBuildContextPayload_OptOut verifies setting session_start_inject=false
// suppresses the hook output entirely.
func TestBuildContextPayload_OptOut(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ctx-optout-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	st := store.NewStore(dir)
	p, _ := st.CreateProject("alpha")
	st.CreateTask(p.ID, "t", "", 2)

	off := false
	out := buildContextPayload(st, &server.Config{SessionStartInject: &off})
	if out != "" {
		t.Errorf("expected empty payload under opt-out, got:\n%s", out)
	}
}

// TestSegmentsContextBlock_NoCWDMatch verifies the no-match stanza is a terse
// one-liner pointing at segments_list_projects -- no verbose project dump.
func TestSegmentsContextBlock_NoCWDMatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ctx-nomatch-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	origWD, _ := os.Getwd()
	cwd := filepath.Join(dir, "nothing-matches")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWD)

	st := store.NewStore(dir)
	st.CreateProject("alpha")
	st.CreateProject("beta")
	projects, _ := st.ListProjects()

	out := segmentsContextBlock(st, projects)
	if !strings.Contains(out, "No Segments project matches CWD basename") {
		t.Errorf("expected terse no-match one-liner, got:\n%s", out)
	}
	if !strings.Contains(out, "segments_list_projects") {
		t.Errorf("expected pointer to segments_list_projects, got:\n%s", out)
	}
	// No project list dump allowed: the old behavior printed every project
	// name, which defeats the compaction goal.
	if strings.Contains(out, "Available projects:") {
		t.Errorf("old verbose project list leaked, got:\n%s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Errorf("project names should NOT be listed, got:\n%s", out)
	}
}

// TestSegmentsContextBlock_NoCWDMatch_GitHint verifies that when CWD has a
// .git directory we append a short pointer to `git log`.
func TestSegmentsContextBlock_NoCWDMatch_GitHint(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-ctx-githint-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("SEGMENTS_DATA_DIR", dir)

	origWD, _ := os.Getwd()
	cwd := filepath.Join(dir, "somerepo")
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWD)

	st := store.NewStore(dir)
	// Two projects prevent resolveProject's single-project fallback from
	// latching on -- we need genuine no-match to hit the git-hint branch.
	st.CreateProject("alpha")
	st.CreateProject("beta")
	projects, _ := st.ListProjects()

	out := segmentsContextBlock(st, projects)
	if !strings.Contains(out, "Git repo detected") {
		t.Errorf("expected git hint when .git present, got:\n%s", out)
	}
	if !strings.Contains(out, "git log") {
		t.Errorf("expected `git log` pointer, got:\n%s", out)
	}
}

// TestClaudeAllowlistIdempotent verifies writeClaudeAllowlist preserves
// unrelated settings, adds mcp__segments once, and is a no-op on re-run.
// removeClaudeAllowlist should strip the entry without clobbering siblings.
func TestClaudeAllowlistIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-allowlist-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "settings.json")
	seed := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []interface{}{"Bash(ls)", "WebFetch"},
			"deny":  []interface{}{"Bash(rm *)"},
		},
		"hooks": map[string]interface{}{"SessionStart": []interface{}{}},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeClaudeAllowlist(path); err != nil {
		t.Fatalf("writeClaudeAllowlist: %v", err)
	}
	if !claudeAllowlistConfigured(path) {
		t.Fatalf("allowlist entry not detected after write")
	}

	// Idempotent: second write should not duplicate.
	if err := writeClaudeAllowlist(path); err != nil {
		t.Fatalf("second writeClaudeAllowlist: %v", err)
	}
	data, _ := os.ReadFile(path)
	var after map[string]interface{}
	json.Unmarshal(data, &after)
	perms, _ := after["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	seen := 0
	for _, e := range allow {
		if s, _ := e.(string); s == "mcp__segments" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("mcp__segments should appear exactly once, got %d (allow=%v)", seen, allow)
	}
	// Unrelated entries preserved.
	haveBash, haveFetch := false, false
	for _, e := range allow {
		switch e {
		case "Bash(ls)":
			haveBash = true
		case "WebFetch":
			haveFetch = true
		}
	}
	if !haveBash || !haveFetch {
		t.Errorf("pre-existing allow entries clobbered: %v", allow)
	}
	if deny, _ := perms["deny"].([]interface{}); len(deny) != 1 || deny[0] != "Bash(rm *)" {
		t.Errorf("deny list clobbered: %v", deny)
	}
	if _, ok := after["hooks"]; !ok {
		t.Errorf("hooks section clobbered")
	}

	// Remove strips the entry without dropping siblings.
	removeClaudeAllowlist(path)
	if claudeAllowlistConfigured(path) {
		t.Errorf("removeClaudeAllowlist did not strip entry")
	}
	data, _ = os.ReadFile(path)
	var final map[string]interface{}
	json.Unmarshal(data, &final)
	fperms, _ := final["permissions"].(map[string]interface{})
	fallow, _ := fperms["allow"].([]interface{})
	if len(fallow) != 2 {
		t.Errorf("remove dropped unrelated entries: %v", fallow)
	}
}

// TestSessionStartInjectEnabled verifies the tri-state helper: nil and
// explicit true both enable; only explicit false disables.
func TestSessionStartInjectEnabled(t *testing.T) {
	if !sessionStartInjectEnabled(nil) {
		t.Errorf("nil config should enable")
	}
	if !sessionStartInjectEnabled(&server.Config{}) {
		t.Errorf("nil SessionStartInject should enable")
	}
	on := true
	if !sessionStartInjectEnabled(&server.Config{SessionStartInject: &on}) {
		t.Errorf("explicit true should enable")
	}
	off := false
	if sessionStartInjectEnabled(&server.Config{SessionStartInject: &off}) {
		t.Errorf("explicit false should disable")
	}
}
