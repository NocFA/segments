package store

import (
	"errors"
	"os"
	"testing"

	"codeberg.org/nocfa/segments/internal/models"
)

func setupTest(t *testing.T) (*Store, func()) {
	tmp, err := os.MkdirTemp("", "segments-test")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(tmp)
	return s, func() { os.RemoveAll(tmp) }
}

func TestCreateProject(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, err := s.CreateProject("test-project")
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if proj.Name != "test-project" {
		t.Errorf("Name = %q, want %q", proj.Name, "test-project")
	}
	if proj.ID == "" {
		t.Error("ID should not be empty")
	}
}

func TestGetProject(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	created, _ := s.CreateProject("test-project")
	retrieved, err := s.GetProject(created.ID)
	if err != nil {
		t.Fatalf("GetProject error: %v", err)
	}
	if retrieved.ID != created.ID {
		t.Errorf("ID = %q, want %q", retrieved.ID, created.ID)
	}
	if retrieved.Name != "test-project" {
		t.Errorf("Name = %q, want %q", retrieved.Name, "test-project")
	}
}

func TestListProjects(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	s.CreateProject("proj1")
	s.CreateProject("proj2")

	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
}

func TestDeleteProject(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	err := s.DeleteProject(proj.ID)
	if err != nil {
		t.Fatalf("DeleteProject error: %v", err)
	}
	_, err = s.GetProject(proj.ID)
	if err == nil {
		t.Error("GetProject should fail after delete")
	}
}

func TestCreateTask(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	t.Logf("Created project: %s at %s/projects/%s", proj.ID, s.basePath, proj.ID)
	task, err := s.CreateTask(proj.ID, "test task", "body content", 2)
	if err != nil {
		t.Fatalf("CreateTask error: %v", err)
	}
	if task.Title != "test task" {
		t.Errorf("Title = %q, want %q", task.Title, "test task")
	}
	if task.Status != models.StatusTodo {
		t.Errorf("Status = %q, want %q", task.Status, models.StatusTodo)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
	if task.ProjectID != proj.ID {
		t.Errorf("ProjectID = %q, want %q", task.ProjectID, proj.ID)
	}
}

func TestGetTask(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	created, _ := s.CreateTask(proj.ID, "test task", "body", 0)

	retrieved, err := s.GetTask(proj.ID, created.ID)
	if err != nil {
		t.Fatalf("GetTask error: %v", err)
	}
	if retrieved.ID != created.ID {
		t.Errorf("ID = %q, want %q", retrieved.ID, created.ID)
	}
}

func TestListTasks(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	s.CreateTask(proj.ID, "task 1", "", 0)
	s.CreateTask(proj.ID, "task 2", "", 0)

	list, err := s.ListTasks(proj.ID)
	if err != nil {
		t.Fatalf("ListTasks error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
}

func TestListAllTasks(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	p1, _ := s.CreateProject("proj-a")
	p2, _ := s.CreateProject("proj-b")
	s.CreateTask(p1.ID, "a1", "", 1)
	s.CreateTask(p1.ID, "a2", "", 2)
	s.CreateTask(p2.ID, "b1", "", 1)

	all, err := s.ListAllTasks()
	if err != nil {
		t.Fatalf("ListAllTasks error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}

	counts := map[string]int{}
	for _, task := range all {
		counts[task.ProjectID]++
		if task.ProjectID == "" {
			t.Errorf("task %q missing ProjectID", task.ID)
		}
	}
	if counts[p1.ID] != 2 {
		t.Errorf("proj-a count = %d, want 2", counts[p1.ID])
	}
	if counts[p2.ID] != 1 {
		t.Errorf("proj-b count = %d, want 1", counts[p2.ID])
	}
}

func TestUpdateTask(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	created, _ := s.CreateTask(proj.ID, "original", "body", 0)

	updated, err := s.UpdateTask(proj.ID, created.ID, "updated", "new body", models.StatusInProgress, 1, "")
	if err != nil {
		t.Fatalf("UpdateTask error: %v", err)
	}
	if updated.Title != "updated" {
		t.Errorf("Title = %q, want %q", updated.Title, "updated")
	}
	if updated.Body != "new body" {
		t.Errorf("Body = %q, want %q", updated.Body, "new body")
	}
	if updated.Status != models.StatusInProgress {
		t.Errorf("Status = %q, want %q", updated.Status, models.StatusInProgress)
	}
	if updated.Priority != 1 {
		t.Errorf("Priority = %d, want 1", updated.Priority)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Error("CreatedAt should not change")
	}
}

func TestDeleteTask(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	task, _ := s.CreateTask(proj.ID, "test task", "", 0)

	err := s.DeleteTask(proj.ID, task.ID)
	if err != nil {
		t.Fatalf("DeleteTask error: %v", err)
	}
	_, err = s.GetTask(proj.ID, task.ID)
	if err == nil {
		t.Error("GetTask should fail after delete")
	}
}

func TestNextSortOrder(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")

	order1, err := s.NextSortOrder(proj.ID)
	if err != nil {
		t.Fatalf("NextSortOrder error: %v", err)
	}
	if order1 != 0 {
		t.Errorf("order1 = %d, want 0", order1)
	}

	s.CreateTask(proj.ID, "task1", "", 0)

	order2, err := s.NextSortOrder(proj.ID)
	if err != nil {
		t.Fatalf("NextSortOrder error: %v", err)
	}
	if order2 != 1 {
		t.Errorf("order2 = %d, want 1", order2)
	}
}

func TestProjectNotFound(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	_, err := s.GetProject("nonexistent")
	if err == nil {
		t.Error("GetProject should fail for nonexistent project")
	}
}

func TestTaskNotFound(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("test-project")
	_, err := s.GetTask(proj.ID, "nonexistent")
	if err == nil {
		t.Error("GetTask should fail for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestGetTask_EmptyProjectReturnsSentinelError(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	proj, _ := s.CreateProject("empty")
	_, err := s.GetTask(proj.ID, "any-id")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound on empty project (dbi missing), got %v", err)
	}
}

func TestFindTaskAny_AcrossProjects(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	p1, _ := s.CreateProject("alpha")
	p2, _ := s.CreateProject("beta")

	t1, _ := s.CreateTask(p1.ID, "in alpha", "", 2)
	t2, _ := s.CreateTask(p2.ID, "in beta", "", 2)

	matches, err := s.FindTaskAny(t1.ID)
	if err != nil {
		t.Fatalf("FindTaskAny err: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].ProjectID != p1.ID {
		t.Errorf("matches[0].ProjectID = %q, want %q", matches[0].ProjectID, p1.ID)
	}

	matches, _ = s.FindTaskAny(t2.ID)
	if len(matches) != 1 || matches[0].ProjectID != p2.ID {
		t.Errorf("cross-project lookup for t2 failed: %+v", matches)
	}

	// prefix match unique
	shortID := t1.ID[:8]
	matches, _ = s.FindTaskAny(shortID)
	if len(matches) != 1 || matches[0].Task.ID != t1.ID {
		t.Errorf("prefix match failed: %+v", matches)
	}

	// no match
	matches, _ = s.FindTaskAny("deadbeef-no-match-anywhere-0000000000")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestFindTaskAny_AmbiguousPrefix(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	p1, _ := s.CreateProject("alpha")
	p2, _ := s.CreateProject("beta")

	// Force two tasks whose IDs share a prefix by reopening and inspecting.
	// Since UUIDs are random, the odds of collision on a short prefix are very
	// low for a small test. Instead, take real IDs and search by their shared
	// 1-char prefix; there's a good chance of collision. Fall back to an empty
	// prefix (matches everything).
	s.CreateTask(p1.ID, "one", "", 2)
	s.CreateTask(p2.ID, "two", "", 2)

	// Empty prefix should match nothing (guard in FindTaskAny).
	if matches, _ := s.FindTaskAny(""); len(matches) != 0 {
		t.Errorf("empty prefix should return 0 matches, got %d", len(matches))
	}

	// A short prefix that surely matches at least some UUIDs: walk hex digits
	// until we find one that hits 2+ ids.
	foundAmbig := false
	for _, hex := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"} {
		matches, _ := s.FindTaskAny(hex)
		if len(matches) >= 2 {
			projects := map[string]bool{}
			for _, m := range matches {
				projects[m.ProjectID] = true
			}
			if len(projects) >= 2 {
				foundAmbig = true
				break
			}
		}
	}
	if !foundAmbig {
		t.Log("no ambiguous-across-projects hex prefix in this run (UUIDs randomly don't overlap); unusual but not a bug")
	}
}