package store

import (
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
}