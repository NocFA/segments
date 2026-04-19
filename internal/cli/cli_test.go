package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"codeberg.org/nocfa/segments/internal/models"
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
