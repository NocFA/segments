package models

import (
	"encoding/json"
	"time"
)

type TaskStatus string

const (
	StatusTodo       TaskStatus = "todo"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
	StatusClosed     TaskStatus = "closed"
	StatusBlocker    TaskStatus = "blocker"
)

type Task struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Title     string     `json:"title"`
	Status    TaskStatus `json:"status"`
	Priority  int        `json:"priority"`
	Body      string     `json:"body"`
	BlockedBy []string   `json:"blocked_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	SortOrder int        `json:"sort_order"`
}

// Legacy bare-string blocked_by (pre-multi-blocker) is lifted into a one-element slice.
func (t *Task) UnmarshalJSON(data []byte) error {
	type rawTask struct {
		ID        string          `json:"id"`
		ProjectID string          `json:"project_id"`
		Title     string          `json:"title"`
		Status    TaskStatus      `json:"status"`
		Priority  int             `json:"priority"`
		Body      string          `json:"body"`
		BlockedBy json.RawMessage `json:"blocked_by,omitempty"`
		CreatedAt time.Time       `json:"created_at"`
		UpdatedAt time.Time       `json:"updated_at"`
		ClosedAt  *time.Time      `json:"closed_at,omitempty"`
		SortOrder int             `json:"sort_order"`
	}
	var raw rawTask
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.ID = raw.ID
	t.ProjectID = raw.ProjectID
	t.Title = raw.Title
	t.Status = raw.Status
	t.Priority = raw.Priority
	t.Body = raw.Body
	t.CreatedAt = raw.CreatedAt
	t.UpdatedAt = raw.UpdatedAt
	t.ClosedAt = raw.ClosedAt
	t.SortOrder = raw.SortOrder
	t.BlockedBy = nil

	if len(raw.BlockedBy) == 0 || string(raw.BlockedBy) == "null" {
		return nil
	}
	if raw.BlockedBy[0] == '[' {
		return json.Unmarshal(raw.BlockedBy, &t.BlockedBy)
	}
	var s string
	if err := json.Unmarshal(raw.BlockedBy, &s); err != nil {
		return err
	}
	if s != "" {
		t.BlockedBy = []string{s}
	}
	return nil
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
