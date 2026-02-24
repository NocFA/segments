package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/store"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port      string `yaml:"port"`
	DataDir   string `yaml:"data_dir"`
	LogFile   string `yaml:"log_file"`
	EnableMCP bool   `yaml:"enable_mcp"`
	Extension string `yaml:"extension"`
}

type Server struct {
	store     *store.Store
	hub       *Hub
	addr      string
	pidFile   string
	mux       *http.ServeMux
	http      *http.Server
	config    *Config
}

func NewServer(store *store.Store, hub *Hub, cfg *Config, pidFile string) *Server {
	s := &Server{
		store:   store,
		hub:     hub,
		addr:    cfg.Port,
		pidFile: pidFile,
		mux:     http.NewServeMux(),
		config:  cfg,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /ws", s.hub.ServeHTTP)
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	s.mux.HandleFunc("GET /api/projects/{id}/tasks", s.handleListTasks)
	s.mux.HandleFunc("POST /api/projects/{id}/tasks", s.handleCreateTask)
	s.mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("PUT /api/tasks/{id}", s.handleUpdateTask)
	s.mux.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	s.mux.HandleFunc("POST /internal/sync", s.handleSync)
	s.mux.HandleFunc("GET /internal/config", s.handleConfig)
	s.mux.HandleFunc("GET /internal/extension", s.handleExtension)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.hub.ServeHTTP(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Start() error {
	addr := s.addr
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	s.http = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	if err := s.writePIDFile(); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	go s.hub.Run()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	fmt.Printf("Server started on %s\n", addr)
	return s.http.Serve(ln)
}

func (s *Server) Shutdown() error {
	if s.pidFile != "" {
		os.Remove(s.pidFile)
	}
	if s.http != nil {
		return s.http.Shutdown(nil)
	}
	return nil
}

func (s *Server) writePIDFile() error {
	if s.pidFile == "" {
		return nil
	}

	dir := filepath.Dir(s.pidFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	pid := fmt.Sprintf("%d\n%s\n", os.Getpid(), s.addr)
	return os.WriteFile(s.pidFile, []byte(pid), 0644)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(indexHTML))
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []models.Project{}
	}
	s.writeJSON(w, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		s.writeError(w, "name is required", http.StatusBadRequest)
		return
	}

	proj, err := s.store.CreateProject(req.Name)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(WSMessage{Type: "project:created", Data: proj})
	s.writeJSON(w, proj)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := s.extractID(r)
	if id == "" {
		s.writeError(w, "id required", http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteProject(id); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(WSMessage{Type: "project:deleted", Data: map[string]string{"id": id}})
	s.writeJSON(w, map[string]string{"deleted": id})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	projectID := s.extractID(r)
	if projectID == "" {
		s.writeError(w, "project id required", http.StatusBadRequest)
		return
	}

	tasks, err := s.store.ListTasks(projectID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}
	s.writeJSON(w, tasks)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	projectID := s.extractID(r)
	if projectID == "" {
		s.writeError(w, "project id required", http.StatusBadRequest)
		return
	}

	var req struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		s.writeError(w, "title is required", http.StatusBadRequest)
		return
	}

	task, err := s.store.CreateTask(projectID, req.Title, req.Body, req.Priority)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(WSMessage{Type: "task:created", Data: task})
	s.writeJSON(w, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := s.extractID(r)
	if taskID == "" {
		s.writeError(w, "task id required", http.StatusBadRequest)
		return
	}

	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		s.writeError(w, "project_id query param required", http.StatusBadRequest)
		return
	}

	task, err := s.store.GetTask(projectID, taskID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}

	s.writeJSON(w, task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID := s.extractID(r)
	if taskID == "" {
		s.writeError(w, "task id required", http.StatusBadRequest)
		return
	}

	var req struct {
		ProjectID string          `json:"project_id"`
		Title     string          `json:"title"`
		Body      string          `json:"body"`
		Status    models.TaskStatus `json:"status"`
		Priority  int             `json:"priority"`
		BlockedBy string          `json:"blocked_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ProjectID == "" {
		s.writeError(w, "project_id is required", http.StatusBadRequest)
		return
	}

	task, err := s.store.UpdateTask(req.ProjectID, taskID, req.Title, req.Body, req.Status, req.Priority, req.BlockedBy)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(WSMessage{Type: "task:updated", Data: task})
	s.writeJSON(w, task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := s.extractID(r)
	if taskID == "" {
		s.writeError(w, "task id required", http.StatusBadRequest)
		return
	}

	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		s.writeError(w, "project_id query param required", http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteTask(projectID, taskID); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.hub.Broadcast(WSMessage{Type: "task:deleted", Data: map[string]string{"id": taskID}})
	s.writeJSON(w, map[string]string{"deleted": taskID})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]interface{}{
		"port":      s.config.Port,
		"data_dir":  s.config.DataDir,
		"enable_mcp": s.config.EnableMCP,
		"extension": s.config.Extension,
	}
	s.writeJSON(w, cfg)
}

func (s *Server) handleExtension(w http.ResponseWriter, r *http.Request) {
	if s.config.Extension == "" {
		s.writeError(w, "no extension detected", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(s.config.Extension)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/typescript")
	w.Header().Set("X-Extension-Path", s.config.Extension)
	w.Write(data)
}

func (s *Server) extractID(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")

	for i, part := range parts {
		if part == "tasks" && i == len(parts)-2 {
			return parts[i+1]
		}
		if part == "projects" && i+1 < len(parts) && parts[i+1] != "tasks" {
			return parts[i+1]
		}
		if part == "tasks" && i > 0 && parts[i-1] != "projects" {
			return parts[i-1]
		}
	}

	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

func (s *Server) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &Config{
			Port:    "8765",
			DataDir: "~/.segments",
		}, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Port == "" {
		cfg.Port = "8765"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "~/.segments"
	}

	cfg = detectExtensions(cfg)


	return &cfg, nil
}

func detectExtensions(cfg Config) Config {
	home, _ := os.UserHomeDir()
	dataDir := ExpandPath(cfg.DataDir)

	mcpPipe := filepath.Join(dataDir, "mcp.stdin")
	if _, err := os.Stat(mcpPipe); err == nil {
		cfg.EnableMCP = true
	}

	piExtPaths := []string{
		filepath.Join(dataDir, "..", ".pi", "extensions", "segments.ts"),
		filepath.Join(home, "Dev", "segments", ".pi", "extensions", "segments.ts"),
	}

	for _, p := range piExtPaths {
		if _, err := os.Stat(p); err == nil {
			cfg.Extension = p
			break
		}
	}

	return cfg
}

func ExpandPath(path string) string {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return expanded
		}
		return filepath.Join(home, expanded[1:])
	}
	return expanded
}

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
}

var _ = io.Discard