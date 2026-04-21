package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/nocfa/segments/internal/analytics"
	"codeberg.org/nocfa/segments/internal/export"
	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/store"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port        string        `yaml:"port" json:"port"`
	Bind        string        `yaml:"bind" json:"bind,omitempty"`
	DataDir     string        `yaml:"data_dir" json:"data_dir"`
	LogFile     string        `yaml:"log_file" json:"log_file,omitempty"`
	EnableMCP   bool          `yaml:"enable_mcp" json:"enable_mcp,omitempty"`
	Extension   string        `yaml:"extension" json:"extension,omitempty"`
	JSONLExport export.Config `yaml:"jsonl_export" json:"jsonl_export,omitempty"`
	// Analytics is a tri-state opt-out for the local event log at
	// ~/.segments/events.jsonl. Unset (nil) defaults to enabled; set to
	// false in config.yaml to disable, or use SEGMENTS_ANALYTICS=0.
	Analytics *bool  `yaml:"analytics" json:"analytics,omitempty"`
	Version   string `yaml:"-" json:"version,omitempty"`
}

type Server struct {
	store    *store.Store
	hub      *Hub
	addr     string
	bind     string
	pidFile  string
	mux      *http.ServeMux
	http     *http.Server
	config   *Config
	exporter *export.Writer
}

func NewServer(store *store.Store, hub *Hub, cfg *Config, pidFile string) *Server {
	s := &Server{
		store:    store,
		hub:      hub,
		addr:     cfg.Port,
		bind:     cfg.Bind,
		pidFile:  pidFile,
		mux:      http.NewServeMux(),
		config:   cfg,
		exporter: export.NewWriter(cfg.JSONLExport),
	}
	s.routes()
	return s
}

func (s *Server) Exporter() *export.Writer {
	return s.exporter
}

func (s *Server) emit(typ string, data interface{}) {
	s.hub.Broadcast(WSMessage{Type: typ, Data: data})
	s.exporter.Emit(typ, data)
	recordAnalyticsEvent(typ, data)
}

// recordAnalyticsEvent extracts task/project IDs from the write payload
// and appends an event tagged source=web to the default analytics writer.
// No agent, since web writes come from the browser.
func recordAnalyticsEvent(typ string, data interface{}) {
	var projectID, taskID, toStatus, recordType string
	recordType = typ
	switch v := data.(type) {
	case *models.Task:
		if v == nil {
			return
		}
		projectID = v.ProjectID
		taskID = v.ID
		toStatus = string(v.Status)
	case models.Task:
		projectID = v.ProjectID
		taskID = v.ID
		toStatus = string(v.Status)
	case *models.Project:
		if v == nil {
			return
		}
		projectID = v.ID
	case models.Project:
		projectID = v.ID
	case map[string]string:
		taskID = v["id"]
		projectID = v["project_id"]
	default:
		return
	}
	if typ == "task:updated" && toStatus != "" {
		switch toStatus {
		case string(models.StatusInProgress):
			recordType = "task:claimed"
		case string(models.StatusDone):
			recordType = "task:completed"
		case string(models.StatusClosed):
			recordType = "task:closed"
		}
	}
	analytics.Record(analytics.Event{
		Type:      recordType,
		Source:    "web",
		ProjectID: projectID,
		TaskID:    taskID,
		ToStatus:  toStatus,
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleRoot)
	s.mux.HandleFunc("GET /ws", s.hub.ServeHTTP)
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("PUT /api/projects/{id}", s.handleUpdateProject)
	s.mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	s.mux.HandleFunc("GET /api/projects/{id}/tasks", s.handleListTasks)
	s.mux.HandleFunc("POST /api/projects/{id}/tasks", s.handleCreateTask)
	s.mux.HandleFunc("GET /api/tasks", s.handleListAllTasks)
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
		addr = s.bind + ":" + addr
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

	addr := s.addr
	if !strings.Contains(addr, ":") {
		addr = s.bind + ":" + addr
	}
	pid := fmt.Sprintf("%d\n%s\n", os.Getpid(), addr)
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

	s.emit("project:created", proj)
	s.writeJSON(w, proj)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := s.extractID(r)
	if id == "" {
		s.writeError(w, "id required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		s.writeError(w, "name required", http.StatusBadRequest)
		return
	}

	proj, err := s.store.UpdateProject(id, req.Name)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.emit("project:updated", proj)
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

	s.emit("project:deleted", map[string]string{"id": id})
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

func (s *Server) handleListAllTasks(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("all") != "1" {
		s.writeError(w, "all=1 query param required", http.StatusBadRequest)
		return
	}

	tasks, err := s.store.ListAllTasks()
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

	s.emit("task:created", task)
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

	s.emit("task:updated", task)
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

	s.emit("task:deleted", map[string]string{"id": taskID, "project_id": projectID})
	s.writeJSON(w, map[string]string{"deleted": taskID})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var msg WSMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err == nil && msg.Type != "" {
		s.hub.Broadcast(msg)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]interface{}{
		"port":       s.config.Port,
		"bind":       s.config.Bind,
		"data_dir":   s.config.DataDir,
		"enable_mcp": s.config.EnableMCP,
		"extension":  s.config.Extension,
		"version":    s.config.Version,
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
	return r.PathValue("id")
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
			Bind:    "127.0.0.1",
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
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "~/.segments"
	}

	cfg = detectExtensions(cfg)


	return &cfg, nil
}

func detectExtensions(cfg Config) Config {
	dataDir := ExpandPath(cfg.DataDir)

	if _, err := os.Stat(filepath.Join(dataDir, "mcp.stdin")); err == nil {
		cfg.EnableMCP = true
	}

	// Check for pi extension relative to data dir, then cwd
	candidates := []string{
		filepath.Join(dataDir, "..", ".pi", "extensions", "segments.ts"),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ".pi", "extensions", "segments.ts"))
	}
	for _, p := range candidates {
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