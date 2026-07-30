package mapbridge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/store"
)

type bridgeFixture struct {
	t       *testing.T
	store   *store.Store
	project models.Project
	bridge  *Bridge
	repo    string
	out     *bytes.Buffer
}

func newBridgeFixture(t *testing.T) bridgeFixture {
	t.Helper()
	base := t.TempDir()
	repo := t.TempDir()
	st := store.NewStore(base)
	t.Cleanup(st.Close)
	p, err := st.CreateProject("Demo Project")
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	b, err := New(Config{Store: st, Project: *p, Repo: repo, Out: out, Now: func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return bridgeFixture{t: t, store: st, project: *p, bridge: b, repo: repo, out: out}
}
func (f bridgeFixture) ticket(name string) string {
	return filepath.Join(f.repo, ".plan", "maps", "demo-project", "tickets", name)
}
func (f bridgeFixture) createTask(title, body string, priority int) *models.Task {
	f.t.Helper()
	task, err := f.store.CreateTask(f.project.ID, title, body, priority)
	if err != nil {
		f.t.Fatal(err)
	}
	// Keep CreatedAt distinct so ticket numbering never falls to tiebreaks.
	time.Sleep(2 * time.Millisecond)
	return task
}
func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func status(t *testing.T, st *store.Store, pid, id string) models.TaskStatus {
	t.Helper()
	task, err := st.GetTask(pid, id)
	if err != nil {
		t.Fatal(err)
	}
	return task.Status
}

func TestNumberAssignmentStabilityAndTitleChangeRename(t *testing.T) {
	f := newBridgeFixture(t)
	first := f.createTask("[scope] 01 First task", "first body", 2)
	second := f.createTask("Second task", "second body", 2)
	blocked := []string{first.ID}
	f.store.UpdateTask(f.project.ID, second.ID, store.TaskPatch{BlockedBy: &blocked})
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.ticket("01-first-task.md")); err != nil {
		t.Fatal(err)
	}
	text := mustRead(t, f.ticket("02-second-task.md"))
	if !strings.Contains(text, "blocked_by: [01]") {
		t.Fatalf("translated blocker missing:\n%s", text)
	}
	renamed := "Renamed second task"
	f.store.UpdateTask(f.project.ID, second.ID, store.TaskPatch{Title: &renamed})
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.ticket("02-second-task.md")); !os.IsNotExist(err) {
		t.Fatalf("old filename still exists: %v", err)
	}
	if _, err := os.Stat(f.ticket("02-renamed-second-task.md")); err != nil {
		t.Fatal(err)
	}
	third := f.createTask("Third", "", 2)
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(f.bridge.statePath, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if state.Numbers[first.ID] != 1 || state.Numbers[second.ID] != 2 || state.Numbers[third.ID] != 3 {
		t.Fatalf("unstable numbers: %#v", state.Numbers)
	}
}

func TestStatusMappingSegmentsToFiles(t *testing.T) {
	f := newBridgeFixture(t)
	ip := f.createTask("Working", "body", 2)
	done := f.createTask("Done", "body", 2)
	closed := f.createTask("Closed", "body", 2)
	s := models.StatusInProgress
	f.store.UpdateTask(f.project.ID, ip.ID, store.TaskPatch{Status: &s})
	s = models.StatusDone
	f.store.UpdateTask(f.project.ID, done.ID, store.TaskPatch{Status: &s})
	s = models.StatusClosed
	f.store.UpdateTask(f.project.ID, closed.ID, store.TaskPatch{Status: &s})
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	if text := mustRead(t, f.ticket("01-working.md")); !strings.Contains(text, "claimed_by: sg") || !strings.Contains(text, "claimed_at: 2026-07-30T12:00:00Z") {
		t.Fatalf("in-progress encoding wrong:\n%s", text)
	}
	if text := mustRead(t, f.ticket("02-done.md")); !strings.Contains(text, "## Answer\n\nResolved in Segments (no recorded answer).") {
		t.Fatalf("done encoding wrong:\n%s", text)
	}
	if text := mustRead(t, f.ticket("03-closed.md")); !strings.Contains(text, "## Ruled out\n\nClosed in Segments.") {
		t.Fatalf("closed encoding wrong:\n%s", text)
	}
}

func TestForeignClaimAndReleaseMapToSegments(t *testing.T) {
	f := newBridgeFixture(t)
	task := f.createTask("Claim me", "body", 2)
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	path := f.ticket("01-claim-me.md")
	text := mustRead(t, path)
	text = strings.Replace(text, "sg: "+task.ID, "sg: "+task.ID+"\nclaimed_by: agent-7\nclaimed_at: 2026-07-30T12:00:00Z", 1)
	mustWrite(t, path, text)
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	if got := status(t, f.store, f.project.ID, task.ID); got != models.StatusInProgress {
		t.Fatalf("claim status=%s", got)
	}
	text = mustRead(t, path)
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "claimed_by:") || strings.HasPrefix(line, "claimed_at:") {
			continue
		}
		kept = append(kept, line)
	}
	mustWrite(t, path, strings.Join(kept, "\n"))
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	if got := status(t, f.store, f.project.ID, task.ID); got != models.StatusTodo {
		t.Fatalf("released status=%s", got)
	}
}

func TestAnswerPromotionAndSynthesizedAnswerIsNotPromoted(t *testing.T) {
	f := newBridgeFixture(t)
	answerable := f.createTask("Choose color", "question", 2)
	resolved := f.createTask("Already done", "original body", 2)
	done := models.StatusDone
	f.store.UpdateTask(f.project.ID, resolved.ID, store.TaskPatch{Status: &done})
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	path := f.ticket("01-choose-color.md")
	mustWrite(t, path, strings.TrimRight(mustRead(t, path), "\n")+"\n\n## Answer\n\nUse blue. It has enough contrast.\n")
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	updated, _ := f.store.GetTask(f.project.ID, answerable.ID)
	if updated.Status != models.StatusDone || !strings.Contains(updated.Body, "Use blue.") {
		t.Fatalf("answer not promoted: %#v", updated)
	}
	mapText := mustRead(t, filepath.Join(f.repo, ".plan", "maps", "demo-project", "map.md"))
	if !strings.Contains(mapText, "[Choose color](tickets/01-choose-color.md) — Use blue.") {
		t.Fatalf("decision missing:\n%s", mapText)
	}
	synthPath := f.ticket("02-already-done.md")
	mustWrite(t, synthPath, strings.Replace(mustRead(t, synthPath), "Resolved in Segments (no recorded answer).", "Resolved in Segments (no recorded answer).   ", 1))
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := f.store.GetTask(f.project.ID, resolved.ID)
	if unchanged.Body != "original body" {
		t.Fatalf("synthesized answer leaked into body: %q", unchanged.Body)
	}
}

func TestBlockedByReverseMergePreservesCrossProjectAndDropsUnknown(t *testing.T) {
	f := newBridgeFixture(t)
	a := f.createTask("A", "", 2)
	b := f.createTask("B", "", 2)
	c := f.createTask("C", "", 2)
	other, _ := f.store.CreateProject("Other")
	cross, _ := f.store.CreateTask(other.ID, "Cross", "", 2)
	initial := []string{a.ID, cross.ID}
	f.store.UpdateTask(f.project.ID, c.ID, store.TaskPatch{BlockedBy: &initial})
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	path := f.ticket("03-c.md")
	text := mustRead(t, path)
	if !strings.Contains(text, "blocked_by: [01]") {
		t.Fatalf("outbound blockers wrong:\n%s", text)
	}
	mustWrite(t, path, strings.Replace(text, "blocked_by: [01]", "blocked_by: [02, 99]", 1))
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	updated, _ := f.store.GetTask(f.project.ID, c.ID)
	want := []string{cross.ID, b.ID}
	if !sameStrings(updated.BlockedBy, want) {
		t.Fatalf("merged blockers=%v want %v", updated.BlockedBy, want)
	}
	if !strings.Contains(f.out.String(), "unknown blocked_by ticket 99 dropped") {
		t.Fatalf("missing warning: %s", f.out.String())
	}
}

func TestNewTicketImportAndEchoSuppression(t *testing.T) {
	f := newBridgeFixture(t)
	existing := f.createTask("Existing", "body", 2)
	if err := f.bridge.Export(); err != nil {
		t.Fatal(err)
	}
	path := f.ticket("01-existing.md")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("echo rewrote unchanged ticket: %v -> %v", before.ModTime(), after.ModTime())
	}
	newPath := f.ticket("07-hand-written.md")
	mustWrite(t, newPath, "---\r\ntype: task\r\nblocked_by: [01]\r\n---\r\n\r\n# Hand written\r\n\r\n## Question\r\n\r\nImported body.\r\n")
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	tasks, err := f.store.ListTasks(f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("task count=%d", len(tasks))
	}
	var imported *models.Task
	for i := range tasks {
		if tasks[i].Title == "Hand written" {
			imported = &tasks[i]
		}
	}
	if imported == nil || imported.Priority != 2 || len(imported.BlockedBy) != 1 || imported.BlockedBy[0] != existing.ID {
		t.Fatalf("bad import: %#v", imported)
	}
	rewritten := mustRead(t, newPath)
	if !strings.Contains(rewritten, "sg: "+imported.ID) {
		t.Fatalf("sg key missing:\n%s", rewritten)
	}
	if strings.Contains(rewritten, "\r") {
		t.Fatal("rewritten ticket is not LF-only")
	}
}

func TestHandWrittenMapCreatesMapTask(t *testing.T) {
	f := newBridgeFixture(t)
	mapDir := filepath.Join(f.repo, ".plan", "maps", "demo-project")
	if err := os.MkdirAll(filepath.Join(mapDir, "tickets"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(mapDir, "map.md"), "# Charted destination\r\n\r\n## Destination\r\n\r\nBuild the bridge.\r\n")
	if err := f.bridge.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	tasks, err := f.store.ListTasks(f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].Title != "[demo-project] map — Charted destination" || tasks[0].Status != models.StatusInProgress || tasks[0].Body != "## Destination\n\nBuild the bridge." {
		t.Fatalf("map task=%#v", tasks[0])
	}
	entries, err := os.ReadDir(filepath.Join(mapDir, "tickets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("map task materialized as ticket: %v", entries)
	}
}
