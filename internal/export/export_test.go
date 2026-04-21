package export

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"codeberg.org/nocfa/segments/internal/models"
)

func tmpPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "segments-export-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "nested", "tasks.jsonl")
}

func readLines(t *testing.T, path string) []Envelope {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var out []Envelope
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var env Envelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal %q: %v", sc.Text(), err)
		}
		out = append(out, env)
	}
	return out
}

func TestDisabledIsNoop(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: false, Path: path})
	w.Emit("task:created", &models.Task{ID: "t1", ProjectID: "p1"})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no file when disabled, got %v", err)
	}
}

func TestEmptyPathIsNoop(t *testing.T) {
	w := NewWriter(Config{Enabled: true, Path: ""})
	if w.Enabled() {
		t.Fatal("enabled with empty path should report disabled")
	}
	w.Emit("task:created", &models.Task{ID: "t1"})
}

func TestEmitAppendsOnePerEvent(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path})
	w.Emit("task:created", &models.Task{ID: "t1", ProjectID: "p1", Title: "a"})
	w.Emit("task:updated", &models.Task{ID: "t1", ProjectID: "p1", Status: models.StatusInProgress})
	w.Emit("task:deleted", map[string]string{"id": "t1", "project_id": "p1"})

	lines := readLines(t, path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0].Event != "task:created" || lines[0].Task == nil || lines[0].Task.ID != "t1" {
		t.Errorf("first line wrong: %+v", lines[0])
	}
	if lines[2].Event != "task:deleted" || lines[2].TaskID != "t1" || lines[2].ProjectID != "p1" {
		t.Errorf("delete envelope wrong: %+v", lines[2])
	}
}

func TestScopeProjectFiltersOut(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path, Scope: ScopeProject, ProjectID: "keep"})
	w.Emit("task:created", &models.Task{ID: "a", ProjectID: "keep"})
	w.Emit("task:created", &models.Task{ID: "b", ProjectID: "drop"})
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0].Task == nil || lines[0].Task.ID != "a" {
		t.Errorf("kept wrong task: %+v", lines[0])
	}
}

func TestScopeProjectAcceptsPrefix(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path, Scope: ScopeProject, ProjectID: "abcd"})
	w.Emit("task:created", &models.Task{ID: "a", ProjectID: "abcd1234-xxxx"})
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (prefix match)", len(lines))
	}
}

func TestOnEventsFilter(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path, OnEvents: []string{"created", "done"}})
	w.Emit("task:created", &models.Task{ID: "a"})
	w.Emit("task:updated", &models.Task{ID: "a", Status: models.StatusInProgress})
	w.Emit("task:updated", &models.Task{ID: "a", Status: models.StatusDone})
	w.Emit("task:deleted", map[string]string{"id": "a"})
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0].Event != "task:created" {
		t.Errorf("line 0 = %q, want task:created", lines[0].Event)
	}
	if lines[1].Event != "task:updated" || lines[1].Task.Status != models.StatusDone {
		t.Errorf("line 1 wrong: %+v", lines[1])
	}
}

func TestConcurrentEmitsAreSerialised(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path})
	var wg sync.WaitGroup
	const N = 50
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.Emit("task:created", &models.Task{ID: "t", ProjectID: "p"})
		}(i)
	}
	wg.Wait()
	lines := readLines(t, path)
	if len(lines) != N {
		t.Fatalf("lines = %d, want %d (serialisation failure)", len(lines), N)
	}
}

func TestSnapshotWritesTasksAndProjects(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path})
	n, err := w.Snapshot(path,
		[]models.Task{{ID: "t1", ProjectID: "p1", Title: "a"}, {ID: "t2", ProjectID: "p1"}},
		[]models.Project{{ID: "p1", Name: "one"}},
	)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if n != 3 {
		t.Fatalf("wrote %d lines, want 3", n)
	}
	lines := readLines(t, path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	if lines[0].Event != "project:snapshot" || lines[0].Project == nil {
		t.Errorf("first line wrong: %+v", lines[0])
	}
	if lines[1].Event != "task:snapshot" || lines[1].Task.ID != "t1" {
		t.Errorf("task line wrong: %+v", lines[1])
	}
}

func TestBuildEnvelopeRejectsUnknown(t *testing.T) {
	if _, ok := buildEnvelope("task:created", 42); ok {
		t.Fatal("int payload should be rejected")
	}
	if _, ok := buildEnvelope("task:created", (*models.Task)(nil)); ok {
		t.Fatal("nil task pointer should be rejected")
	}
}

func TestResolvePathHandlesHomeAndRelative(t *testing.T) {
	home, _ := os.UserHomeDir()
	got, err := ResolvePath("~/x.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(home, "x.jsonl"))
	if got != want {
		t.Errorf("~ expansion got %q want %q", got, want)
	}
	got, err = ResolvePath("rel/x.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	want = filepath.Clean(filepath.Join(cwd, "rel/x.jsonl"))
	if got != want {
		t.Errorf("relative got %q want %q", got, want)
	}
}

func TestEmitBatchFansOut(t *testing.T) {
	path := tmpPath(t)
	w := NewWriter(Config{Enabled: true, Path: path})
	tasks := []*models.Task{
		{ID: "a", ProjectID: "p"},
		{ID: "b", ProjectID: "p"},
		nil,
		{ID: "c", ProjectID: "p"},
	}
	w.EmitBatch("task:created", tasks)
	lines := readLines(t, path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (nil skipped)", len(lines))
	}
}
