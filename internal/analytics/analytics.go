package analytics

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Agent identifies the client that originated an event. Populated for MCP
// tool calls from the initialize params.clientInfo; nil for CLI invocations
// and web UI writes.
type Agent struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Event is one line in the jsonl events log. Kept deliberately small so
// the log stays grep-friendly and cheap to append.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Agent     *Agent    `json:"agent,omitempty"`
	ProjectID string    `json:"project_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	ToStatus  string    `json:"to_status,omitempty"`
}

type Writer struct {
	path    string
	enabled bool
	mu      sync.Mutex
}

func NewWriter(path string, enabled bool) *Writer {
	return &Writer{path: path, enabled: enabled}
}

func (w *Writer) Enabled() bool {
	return w != nil && w.enabled && w.path != ""
}

func (w *Writer) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// Record appends one event to the writer's file. Errors are logged, never
// surfaced: analytics must not fail the user's primary action.
// TODO: rotate events.jsonl when it exceeds 10MB.
func (w *Writer) Record(e Event) {
	if !w.Enabled() {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		log.Printf("analytics: marshal: %v", err)
		return
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		log.Printf("analytics: mkdir: %v", err)
		return
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("analytics: open: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		log.Printf("analytics: write: %v", err)
	}
}

// Read parses the jsonl events file in file order. A missing file is not
// an error; ReadEvents returns (nil, nil). Malformed lines are skipped so
// a single bad line does not poison the whole log.
func Read(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		return events, err
	}
	return events, nil
}

var (
	defaultMu sync.RWMutex
	defaultW  *Writer
)

// SetDefault configures the process-wide writer. Callers (cli.Run) invoke
// it once at startup so subsequent Record calls go somewhere sensible.
func SetDefault(w *Writer) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultW = w
}

func Default() *Writer {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultW
}

// Record writes to the default writer. No-op if no default is set or the
// default writer is disabled.
func Record(e Event) {
	defaultMu.RLock()
	w := defaultW
	defaultMu.RUnlock()
	if w != nil {
		w.Record(e)
	}
}
