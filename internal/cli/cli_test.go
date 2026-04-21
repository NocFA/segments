package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/nocfa/segments/internal/analytics"
	"codeberg.org/nocfa/segments/internal/export"
	"codeberg.org/nocfa/segments/internal/models"
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

func TestGroupTasksForContext_UnblockedTodoIsReady(t *testing.T) {
	tasks := []models.Task{
		{ID: "a", Title: "unblocked", Status: models.StatusTodo},
	}
	g := groupTasksForContext(tasks)
	if len(g.ready) != 1 {
		t.Fatalf("ready: got %d, want 1 (%v)", len(g.ready), g.ready)
	}
	if len(g.blocked) != 0 {
		t.Fatalf("blocked: got %d, want 0 (%v)", len(g.blocked), g.blocked)
	}
	if g.todoCount != 1 {
		t.Fatalf("todoCount: got %d, want 1", g.todoCount)
	}
	if !strings.Contains(g.ready[0], "task_id=a") {
		t.Fatalf("ready entry missing task_id: %q", g.ready[0])
	}
}

func TestGroupTasksForContext_PendingBlockerKeepsTodoBlocked(t *testing.T) {
	tasks := []models.Task{
		{ID: "boot", Title: "init", Status: models.StatusTodo},
		{ID: "child", Title: "downstream", Status: models.StatusTodo, BlockedBy: "boot"},
	}
	g := groupTasksForContext(tasks)
	if len(g.ready) != 1 || !strings.Contains(g.ready[0], "task_id=boot") {
		t.Fatalf("expected only bootstrap ready, got %v", g.ready)
	}
	if len(g.blocked) != 1 || !strings.Contains(g.blocked[0], "task_id=child") {
		t.Fatalf("expected child blocked, got %v", g.blocked)
	}
}

func TestGroupTasksForContext_DoneBlockerUnblocksChild(t *testing.T) {
	tasks := []models.Task{
		{ID: "boot", Title: "init", Status: models.StatusDone},
		{ID: "child", Title: "downstream", Status: models.StatusTodo, BlockedBy: "boot"},
	}
	g := groupTasksForContext(tasks)
	if len(g.ready) != 1 || !strings.Contains(g.ready[0], "task_id=child") {
		t.Fatalf("expected child ready after blocker done, got %v", g.ready)
	}
	if len(g.blocked) != 0 {
		t.Fatalf("expected no blocked, got %v", g.blocked)
	}
	if g.doneCount != 1 {
		t.Fatalf("doneCount: got %d, want 1", g.doneCount)
	}
}

func TestGroupTasksForContext_AllStatusesPartitioned(t *testing.T) {
	tasks := []models.Task{
		{ID: "1", Title: "ready", Status: models.StatusTodo, Priority: 1},
		{ID: "2", Title: "blocked", Status: models.StatusTodo, BlockedBy: "99"},
		{ID: "3", Title: "working", Status: models.StatusInProgress, Priority: 2},
		{ID: "4", Title: "finished", Status: models.StatusDone},
		{ID: "5", Title: "wall", Status: models.StatusBlocker},
		{ID: "6", Title: "dropped", Status: models.StatusClosed},
	}
	g := groupTasksForContext(tasks)
	if len(g.ready) != 1 || !strings.Contains(g.ready[0], " P1") {
		t.Fatalf("ready partition / priority annotation wrong: %v", g.ready)
	}
	if len(g.blocked) != 1 || !strings.Contains(g.blocked[0], "blocked_by=99") {
		t.Fatalf("blocked partition / blocked_by annotation wrong: %v", g.blocked)
	}
	if len(g.inProgress) != 1 || !strings.Contains(g.inProgress[0], "[in_progress]") {
		t.Fatalf("inProgress partition wrong: %v", g.inProgress)
	}
	if len(g.blockers) != 1 || !strings.Contains(g.blockers[0], "[blocker]") {
		t.Fatalf("blockers partition wrong: %v", g.blockers)
	}
	if g.todoCount != 2 || g.inProgressCount != 1 || g.doneCount != 1 || g.blockerCount != 1 {
		t.Fatalf("counts wrong: todo=%d inProg=%d done=%d blocker=%d", g.todoCount, g.inProgressCount, g.doneCount, g.blockerCount)
	}
}

func TestGroupTasksForContext_PreservesInputOrder(t *testing.T) {
	tasks := []models.Task{
		{ID: "a", Title: "first", Status: models.StatusTodo},
		{ID: "b", Title: "second", Status: models.StatusTodo},
		{ID: "c", Title: "third", Status: models.StatusTodo},
	}
	g := groupTasksForContext(tasks)
	if len(g.ready) != 3 {
		t.Fatalf("expected 3 ready, got %d", len(g.ready))
	}
	if !strings.Contains(g.ready[0], "task_id=a") ||
		!strings.Contains(g.ready[1], "task_id=b") ||
		!strings.Contains(g.ready[2], "task_id=c") {
		t.Fatalf("input order not preserved: %v", g.ready)
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
	for name, text := range map[string]string{
		"mcpServerInstructions":        mcpServerInstructions,
		"segmentsShortcutInstructions": segmentsShortcutInstructions,
	} {
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
		} {
			if !strings.Contains(text, cue) {
				t.Errorf("%s: missing cue %q", name, cue)
			}
		}
	}
	if !strings.Contains(segmentsShortcutInstructions, "Ready queue") {
		t.Errorf("segmentsShortcutInstructions: missing 'Ready queue' idiom")
	}
	if !strings.Contains(segmentsShortcutInstructions, "discovered-from") {
		t.Errorf("segmentsShortcutInstructions: missing 'discovered-from' idiom")
	}
	// The old "at most one in_progress at a time" rule was removed when the
	// claim semantic landed -- it conflicted with bulk-claiming a sequence of
	// tasks. If someone reintroduces it, fail loudly.
	for _, text := range []string{mcpServerInstructions, segmentsShortcutInstructions} {
		if strings.Contains(text, "at most one in_progress") {
			t.Errorf("stale 'at most one in_progress' phrasing is back in the prompt; claim semantic permits multiple in_progress")
		}
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
