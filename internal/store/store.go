package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
	"github.com/bmatsuo/lmdb-go/lmdb"
	"github.com/google/uuid"
)

var ErrTaskNotFound = errors.New("task not found")

const (
	initialMapSize int64 = 8 << 20
	maxMapSize     int64 = 2 << 30
)

type TaskMatch struct {
	Task      *models.Task
	ProjectID string
}

type Store struct {
	basePath string
	envs     sync.Map
}

type envCache struct {
	env *lmdb.Env
}

func NewStore(basePath string) *Store {
	return &Store{basePath: basePath}
}

func (s *Store) BasePath() string {
	return s.basePath
}

func (s *Store) openEnv(projectID string) (*lmdb.Env, error) {
	if v, ok := s.envs.Load(projectID); ok {
		ec := v.(*envCache)
		return ec.env, nil
	}

	envPath := filepath.Join(s.basePath, "projects", projectID)
	if err := os.MkdirAll(envPath, 0755); err != nil {
		return nil, err
	}

	env, err := lmdb.NewEnv()
	if err != nil {
		return nil, err
	}

	err = env.SetMapSize(initialMapSize)
	if err != nil {
		return nil, err
	}

	err = env.SetMaxDBs(2)
	if err != nil {
		return nil, err
	}

	err = env.Open(envPath, 0, 0664)
	if err != nil {
		env, err = lmdb.NewEnv()
		if err != nil {
			return nil, err
		}
		env.SetMapSize(initialMapSize)
		env.SetMaxDBs(2)
		err = env.Open(envPath, lmdb.Create, 0664)
		if err != nil {
			return nil, err
		}
	}

	s.envs.Store(projectID, &envCache{env: env})
	return env, nil
}

func updateWithGrow(env *lmdb.Env, fn lmdb.TxnOp) error {
	for {
		err := env.Update(fn)
		if err == nil {
			return nil
		}
		if !lmdb.IsMapFull(err) {
			return err
		}
		info, ierr := env.Info()
		if ierr != nil {
			return err
		}
		if info.MapSize >= maxMapSize {
			return err
		}
		next := info.MapSize * 2
		if next > maxMapSize {
			next = maxMapSize
		}
		if serr := env.SetMapSize(next); serr != nil {
			return err
		}
	}
}

func (s *Store) Close() {
	s.envs.Range(func(key, value interface{}) bool {
		s.envs.Delete(key)
		if ec, ok := value.(*envCache); ok && ec.env != nil {
			ec.env.Close()
		}
		return true
	})
}

func (s *Store) CreateProject(name string) (*models.Project, error) {
	id := uuid.New().String()
	now := time.Now()

	proj := &models.Project{
		ID:        id,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(proj)
	if err != nil {
		return nil, err
	}

	projPath := filepath.Join(s.basePath, "projects", id)
	if err := os.MkdirAll(projPath, 0755); err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(projPath, "meta.json"), data, 0644); err != nil {
		return nil, err
	}

	return proj, nil
}

func (s *Store) GetProject(id string) (*models.Project, error) {
	data, err := os.ReadFile(filepath.Join(s.basePath, "projects", id, "meta.json"))
	if err != nil {
		return nil, err
	}

	var proj models.Project
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, err
	}

	return &proj, nil
}

func (s *Store) ListProjects() ([]models.Project, error) {
	projDir := filepath.Join(s.basePath, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []models.Project
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		proj, err := s.GetProject(entry.Name())
		if err != nil {
			continue
		}

		projects = append(projects, *proj)
	}

	return projects, nil
}

func (s *Store) UpdateProject(id, name string) (*models.Project, error) {
	proj, err := s.GetProject(id)
	if err != nil {
		return nil, err
	}

	proj.Name = name
	proj.UpdatedAt = time.Now()

	data, err := json.Marshal(proj)
	if err != nil {
		return nil, err
	}

	return proj, os.WriteFile(filepath.Join(s.basePath, "projects", id, "meta.json"), data, 0644)
}

func (s *Store) DeleteProject(id string) error {
	// Close the cached LMDB env before removing files, otherwise the data
	// file is locked on Windows.
	if v, ok := s.envs.LoadAndDelete(id); ok {
		if ec, ok := v.(*envCache); ok && ec.env != nil {
			ec.env.Close()
		}
	}

	projPath := filepath.Join(s.basePath, "projects", id)
	return os.RemoveAll(projPath)
}

func (s *Store) NextSortOrder(projectID string) (int, error) {
	env, err := s.openEnv(projectID)
	if err != nil {
		return 0, err
	}

	var next int
	err = env.View(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			if lmdb.IsNotFound(err) {
				next = 0
				return nil
			}
			return err
		}

		cursor, err := txn.OpenCursor(dbi)
		if err != nil {
			return err
		}
		defer cursor.Close()

		var lastKey []byte
		for {
			k, _, err := cursor.Get(nil, nil, lmdb.Next)
			if lmdb.IsNotFound(err) {
				break
			}
			if err != nil {
				return err
			}
			lastKey = k
		}

		if lastKey == nil {
			next = 0
			return nil
		}

		data, err := txn.Get(dbi, lastKey)
		if err != nil {
			return err
		}

		var task models.Task
		if err := json.Unmarshal(data, &task); err != nil {
			return err
		}

		next = task.SortOrder + 1
		return nil
	})

	return next, err
}

func (s *Store) ensureTasksDBI(env *lmdb.Env) (lmdb.DBI, error) {
	var dbi lmdb.DBI
	err := updateWithGrow(env, func(txn *lmdb.Txn) error {
		var err error
		dbi, err = txn.OpenDBI("tasks", lmdb.Create)
		return err
	})
	return dbi, err
}

func (s *Store) CreateTask(projectID, title, body string, priority int) (*models.Task, error) {
	sortOrder, err := s.NextSortOrder(projectID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	task := &models.Task{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Title:     title,
		Status:    models.StatusTodo,
		Priority:  priority,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
		SortOrder: sortOrder,
	}

	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}

	env, err := s.openEnv(projectID)
	if err != nil {
		return nil, err
	}

	dbi, err := s.ensureTasksDBI(env)
	if err != nil {
		return nil, err
	}

	err = updateWithGrow(env, func(txn *lmdb.Txn) error {
		return txn.Put(dbi, []byte(task.ID), data, 0)
	})

	if err != nil {
		return nil, err
	}

	return task, nil
}

func (s *Store) GetTask(projectID, taskID string) (*models.Task, error) {
	env, err := s.openEnv(projectID)
	if err != nil {
		return nil, err
	}

	var task models.Task
	err = env.View(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			if lmdb.IsNotFound(err) {
				return ErrTaskNotFound
			}
			return err
		}

		data, err := txn.Get(dbi, []byte(taskID))
		if err != nil {
			if lmdb.IsNotFound(err) {
				return ErrTaskNotFound
			}
			return err
		}

		return json.Unmarshal(data, &task)
	})

	if err != nil {
		return nil, err
	}

	return &task, nil
}

// FindTaskAny scans every project for tasks whose ID equals or is prefixed by
// idOrPrefix. Returns all matches so callers can decide how to handle 0, 1, or
// many (ambiguous prefix).
func (s *Store) FindTaskAny(idOrPrefix string) ([]TaskMatch, error) {
	if idOrPrefix == "" {
		return nil, nil
	}
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	var matches []TaskMatch
	for _, p := range projects {
		tasks, _ := s.ListTasks(p.ID)
		for i := range tasks {
			if strings.HasPrefix(tasks[i].ID, idOrPrefix) {
				t := tasks[i]
				matches = append(matches, TaskMatch{Task: &t, ProjectID: p.ID})
			}
		}
	}
	return matches, nil
}

func (s *Store) ListAllTasks() ([]models.Task, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}

	var out []models.Task
	for _, p := range projects {
		tasks, err := s.ListTasks(p.ID)
		if err != nil {
			continue
		}
		for i := range tasks {
			if tasks[i].ProjectID == "" {
				tasks[i].ProjectID = p.ID
			}
		}
		out = append(out, tasks...)
	}
	return out, nil
}

func (s *Store) ListTasks(projectID string) ([]models.Task, error) {
	env, err := s.openEnv(projectID)
	if err != nil {
		return nil, err
	}

	var tasks []models.Task
	err = env.View(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			if lmdb.IsNotFound(err) {
				tasks = []models.Task{}
				return nil
			}
			return err
		}

		cursor, err := txn.OpenCursor(dbi)
		if err != nil {
			return err
		}
		defer cursor.Close()

		for {
			k, v, err := cursor.Get(nil, nil, lmdb.Next)
			if lmdb.IsNotFound(err) {
				break
			}
			if err != nil {
				return err
			}

			if k == nil {
				continue
			}

			var task models.Task
			if err := json.Unmarshal(v, &task); err != nil {
				continue
			}

			tasks = append(tasks, task)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return tasks, nil
}

type TaskPatch struct {
	Title     *string
	Body      *string
	Status    *models.TaskStatus
	Priority  *int
	BlockedBy *[]string
}

func (s *Store) UpdateTask(projectID, taskID string, patch TaskPatch) (*models.Task, error) {
	existing, err := s.GetTask(projectID, taskID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	updated := &models.Task{
		ID:        taskID,
		ProjectID: projectID,
		Title:     existing.Title,
		Status:    existing.Status,
		Priority:  existing.Priority,
		Body:      existing.Body,
		BlockedBy: existing.BlockedBy,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
		ClosedAt:  existing.ClosedAt,
		SortOrder: existing.SortOrder,
	}
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.Body != nil {
		updated.Body = *patch.Body
	}
	if patch.Status != nil {
		updated.Status = *patch.Status
	}
	if patch.Priority != nil {
		updated.Priority = *patch.Priority
	}
	if patch.BlockedBy != nil {
		updated.BlockedBy = append([]string(nil), (*patch.BlockedBy)...)
	}

	if (updated.Status == models.StatusDone || updated.Status == models.StatusClosed) && existing.ClosedAt == nil {
		updated.ClosedAt = &now
	} else if updated.Status != models.StatusDone && updated.Status != models.StatusClosed {
		updated.ClosedAt = nil
	}

	data, err := json.Marshal(updated)
	if err != nil {
		return nil, err
	}

	env, err := s.openEnv(projectID)
	if err != nil {
		return nil, err
	}

	err = updateWithGrow(env, func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			return err
		}

		return txn.Put(dbi, []byte(taskID), data, 0)
	})

	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (s *Store) DeleteTask(projectID, taskID string) error {
	env, err := s.openEnv(projectID)
	if err != nil {
		return err
	}

	return updateWithGrow(env, func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			return err
		}

		return txn.Del(dbi, []byte(taskID), nil)
	})
}

func (s *Store) Compact(projectID string) (int64, int64, error) {
	projDir := filepath.Join(s.basePath, "projects", projectID)
	dataPath := filepath.Join(projDir, "data.mdb")
	lockPath := filepath.Join(projDir, "lock.mdb")

	env, err := s.openEnv(projectID)
	if err != nil {
		return 0, 0, err
	}

	before, err := fileSize(dataPath)
	if err != nil {
		return 0, 0, err
	}

	stageDir := filepath.Join(s.basePath, "projects", projectID+".compact")
	if err := os.RemoveAll(stageDir); err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return 0, 0, err
	}

	if err := env.CopyFlag(stageDir, lmdb.CopyCompact); err != nil {
		os.RemoveAll(stageDir)
		return 0, 0, err
	}

	if v, ok := s.envs.LoadAndDelete(projectID); ok {
		if ec, ok := v.(*envCache); ok && ec.env != nil {
			ec.env.Close()
		}
	}

	if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
		os.RemoveAll(stageDir)
		return 0, 0, err
	}
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		os.RemoveAll(stageDir)
		return 0, 0, err
	}

	stageData := filepath.Join(stageDir, "data.mdb")
	if err := os.Rename(stageData, dataPath); err != nil {
		os.RemoveAll(stageDir)
		return 0, 0, err
	}
	os.RemoveAll(stageDir)

	after, err := fileSize(dataPath)
	if err != nil {
		return before, 0, err
	}
	return before, after, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}