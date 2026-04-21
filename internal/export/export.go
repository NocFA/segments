package export

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
)

const (
	ScopeAll     = "all"
	ScopeProject = "project"
)

type Config struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	Path      string   `yaml:"path" json:"path,omitempty"`
	OnEvents  []string `yaml:"on_events" json:"on_events,omitempty"`
	Scope     string   `yaml:"scope" json:"scope,omitempty"`
	ProjectID string   `yaml:"project_id" json:"project_id,omitempty"`
}

type Envelope struct {
	Event     string          `json:"event"`
	Timestamp time.Time       `json:"ts"`
	Task      *models.Task    `json:"task,omitempty"`
	Project   *models.Project `json:"project,omitempty"`
	TaskID    string          `json:"task_id,omitempty"`
	ProjectID string          `json:"project_id,omitempty"`
}

type Writer struct {
	cfg Config
	mu  sync.Mutex
}

func NewWriter(cfg Config) *Writer {
	return &Writer{cfg: cfg}
}

func (w *Writer) Config() Config {
	return w.cfg
}

func (w *Writer) Enabled() bool {
	return w != nil && w.cfg.Enabled && strings.TrimSpace(w.cfg.Path) != ""
}

func (w *Writer) Emit(event string, data interface{}) {
	if !w.Enabled() {
		return
	}
	env, ok := buildEnvelope(event, data)
	if !ok {
		return
	}
	if !w.allowed(env) {
		return
	}
	if err := w.append(env); err != nil {
		log.Printf("jsonl export: %v", err)
	}
}

func (w *Writer) Snapshot(path string, tasks []models.Task, projects []models.Project) (int, error) {
	if strings.TrimSpace(path) == "" {
		return 0, fmt.Errorf("path is required")
	}
	resolved, err := ResolvePath(path)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	now := time.Now().UTC()
	count := 0
	for i := range projects {
		p := projects[i]
		line, err := json.Marshal(Envelope{Event: "project:snapshot", Timestamp: now, Project: &p})
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return count, err
		}
		count++
	}
	for i := range tasks {
		t := tasks[i]
		line, err := json.Marshal(Envelope{Event: "task:snapshot", Timestamp: now, Task: &t})
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (w *Writer) append(env Envelope) error {
	path, err := ResolvePath(w.cfg.Path)
	if err != nil {
		return err
	}
	line, err := json.Marshal(env)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func (w *Writer) allowed(env Envelope) bool {
	if scope := strings.TrimSpace(w.cfg.Scope); scope == ScopeProject {
		pid := w.cfg.ProjectID
		if pid == "" {
			return false
		}
		target := envelopeProjectID(env)
		if target == "" || !strings.HasPrefix(target, pid) {
			return false
		}
	}
	if len(w.cfg.OnEvents) == 0 {
		return true
	}
	cat := eventCategory(env)
	for _, e := range w.cfg.OnEvents {
		if strings.EqualFold(strings.TrimSpace(e), cat) {
			return true
		}
	}
	return false
}

func buildEnvelope(event string, data interface{}) (Envelope, bool) {
	env := Envelope{Event: event, Timestamp: time.Now().UTC()}
	switch v := data.(type) {
	case *models.Task:
		if v == nil {
			return env, false
		}
		env.Task = v
		env.ProjectID = v.ProjectID
		return env, true
	case models.Task:
		env.Task = &v
		env.ProjectID = v.ProjectID
		return env, true
	case []*models.Task:
		return env, false
	case *models.Project:
		if v == nil {
			return env, false
		}
		env.Project = v
		env.ProjectID = v.ID
		return env, true
	case models.Project:
		env.Project = &v
		env.ProjectID = v.ID
		return env, true
	case map[string]string:
		if id, ok := v["id"]; ok {
			env.TaskID = id
		}
		if pid, ok := v["project_id"]; ok {
			env.ProjectID = pid
		}
		return env, true
	case map[string]interface{}:
		if id, ok := v["id"].(string); ok {
			env.TaskID = id
		}
		if pid, ok := v["project_id"].(string); ok {
			env.ProjectID = pid
		}
		return env, true
	}
	return env, false
}

// EmitBatch fans out a slice of tasks as individual task:created lines so a
// bulk create produces one snapshot line per task.
func (w *Writer) EmitBatch(event string, tasks []*models.Task) {
	if !w.Enabled() {
		return
	}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		w.Emit(event, t)
	}
}

func envelopeProjectID(env Envelope) string {
	if env.ProjectID != "" {
		return env.ProjectID
	}
	if env.Task != nil {
		return env.Task.ProjectID
	}
	if env.Project != nil {
		return env.Project.ID
	}
	return ""
}

func eventCategory(env Envelope) string {
	parts := strings.SplitN(env.Event, ":", 2)
	if len(parts) != 2 {
		return env.Event
	}
	action := parts[1]
	if action == "updated" && env.Task != nil {
		switch env.Task.Status {
		case models.StatusDone:
			return "done"
		case models.StatusClosed:
			return "closed"
		}
	}
	return action
}

func ResolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}
