package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
	"github.com/bmatsuo/lmdb-go/lmdb"
	"github.com/google/uuid"
)

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

	err = env.SetMapSize(1 << 30)
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
		env.SetMapSize(1 << 30)
		env.SetMaxDBs(2)
		err = env.Open(envPath, lmdb.Create, 0664)
		if err != nil {
			return nil, err
		}
	}

	s.envs.Store(projectID, &envCache{env: env})
	return env, nil
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

func (s *Store) DeleteProject(id string) error {
	projPath := filepath.Join(s.basePath, "projects", id)
	if err := os.RemoveAll(projPath); err != nil {
		return err
	}

	s.envs.Delete(id)
	return nil
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
	err := env.Update(func(txn *lmdb.Txn) error {
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

	err = env.Update(func(txn *lmdb.Txn) error {
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
			return err
		}

		data, err := txn.Get(dbi, []byte(taskID))
		if err != nil {
			return err
		}

		return json.Unmarshal(data, &task)
	})

	if err != nil {
		return nil, err
	}

	return &task, nil
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

func (s *Store) UpdateTask(projectID, taskID string, title, body string, status models.TaskStatus, priority int, blockedBy string) (*models.Task, error) {
	existing, err := s.GetTask(projectID, taskID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	blockedByVal := blockedBy
	if blockedByVal == "" {
		blockedByVal = existing.BlockedBy
	}
	updated := &models.Task{
		ID:        taskID,
		ProjectID: projectID,
		Title:     title,
		Status:    status,
		Priority:  priority,
		Body:      body,
		BlockedBy: blockedByVal,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
		SortOrder: existing.SortOrder,
	}

	data, err := json.Marshal(updated)
	if err != nil {
		return nil, err
	}

	env, err := s.openEnv(projectID)
	if err != nil {
		return nil, err
	}

	err = env.Update(func(txn *lmdb.Txn) error {
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

	return env.Update(func(txn *lmdb.Txn) error {
		dbi, err := txn.OpenDBI("tasks", 0)
		if err != nil {
			return err
		}

		return txn.Del(dbi, []byte(taskID), nil)
	})
}