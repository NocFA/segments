package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriter_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	w := NewWriter(p, true)

	w.Record(Event{
		Type:      "task:created",
		Source:    "cli",
		ProjectID: "pid",
		TaskID:    "tid",
	})
	w.Record(Event{
		Type:      "task:claimed",
		Source:    "mcp",
		Agent:     &Agent{Name: "claude-code", Version: "1.0.0"},
		ProjectID: "pid",
		TaskID:    "tid",
		ToStatus:  "in_progress",
	})

	events, err := Read(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "task:created" || events[0].Source != "cli" {
		t.Fatalf("event 0 wrong: %+v", events[0])
	}
	if events[1].Agent == nil || events[1].Agent.Name != "claude-code" {
		t.Fatalf("event 1 agent wrong: %+v", events[1])
	}
	if events[0].Timestamp.IsZero() || time.Since(events[0].Timestamp) > time.Minute {
		t.Fatalf("timestamp should be stamped and recent, got %v", events[0].Timestamp)
	}
}

func TestWriter_DisabledIsNoOp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	w := NewWriter(p, false)
	w.Record(Event{Type: "x"})
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("disabled writer should not create file: %v", err)
	}
}

func TestRead_Missing(t *testing.T) {
	events, err := Read(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatalf("read missing: %v", err)
	}
	if events != nil {
		t.Fatalf("got %v, want nil", events)
	}
}

func TestRead_SkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	content := `{"type":"task:created","source":"cli","ts":"2026-04-21T17:00:00Z"}
not json, skip me
{"type":"task:completed","source":"mcp","ts":"2026-04-21T17:05:00Z"}
`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	events, err := Read(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (bad line should be skipped): %+v", len(events), events)
	}
	if events[0].Type != "task:created" || events[1].Type != "task:completed" {
		t.Fatalf("wrong events: %+v", events)
	}
}

func TestDefault_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	SetDefault(NewWriter(p, true))
	defer SetDefault(nil)

	Record(Event{Type: "task:created", Source: "cli"})
	events, err := Read(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 1 || events[0].Type != "task:created" {
		t.Fatalf("got %+v", events)
	}
}
