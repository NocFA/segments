package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/server"
	"codeberg.org/nocfa/segments/internal/store"
)

var dataDir = func() string {
	if d := os.Getenv("SEGMENTS_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".segments")
}()

var pidFile = filepath.Join(dataDir, "pid")

func pidFileData() (int, string, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("invalid pid file")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", err
	}
	port := strings.TrimSpace(lines[1])
	return pid, port, nil
}

func notifyServer() {
	pid, port, err := pidFileData()
	if err != nil {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Pid != pid {
		return
	}
	http.Post("http://localhost:"+port+"/internal/sync", "application/json", bytes.NewReader(nil))
}

func Run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: segments <command>")
	}

	cmd := args[1]
	s := store.NewStore(expandPath(dataDir))

	switch cmd {
	case "serve":
		return runServe(s)
	case "init":
		return runInit(s)
	case "projects":
		return runProjects(s, args[2:])
	case "tasks":
		return runTasks(s, args[2:])
	case "add":
		return runAdd(s, args[2:])
	case "done":
		return runDone(s, args[2:])
	case "update":
		return runUpdate(s, args[2:])
	case "rm":
		return runRm(s, args[2:])
	case "status":
		return runStatus(s)
	case "mcp":
		return runMCP(s)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func runServe(s *store.Store) error {
	cfg, err := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
	}

	dir := expandPath(cfg.DataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	s = store.NewStore(dir)
	hub := server.NewHub()
	srv := server.NewServer(s, hub, cfg.Port, pidFile)

	fmt.Println("Starting Segments server...")
	return srv.Start()
}

func runInit(s *store.Store) error {
	if err := os.MkdirAll(expandPath(dataDir), 0755); err != nil {
		return err
	}
	cfg := server.Config{Port: "8765", DataDir: "~/.segments"}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(dataDir, "config.yaml"), data, 0644)
}

func runProjects(s *store.Store, args []string) error {
	if len(args) == 0 {
		list, err := s.ListProjects()
		if err != nil {
			return err
		}
		for _, p := range list {
			fmt.Printf("%s %s\n", p.ID, p.Name)
		}
		return nil
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: segments projects add <name>")
		}
		p, err := s.CreateProject(args[1])
		if err != nil {
			return err
		}
		fmt.Println(p.ID)
		notifyServer()
		return nil
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: segments projects rm <id>")
		}
		if err := s.DeleteProject(args[1]); err != nil {
			return err
		}
		notifyServer()
		return nil
	default:
		return fmt.Errorf("unknown projects command: %s", args[0])
	}
}

func runTasks(s *store.Store, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: segments tasks <project-id>")
	}
	projectID := args[0]
	list, err := s.ListTasks(projectID)
	if err != nil {
		return err
	}
	for _, t := range list {
		status := string(t.Status)
		fmt.Printf("%s [%s] %s (P%d)\n", t.ID, status, t.Title, t.Priority)
	}
	return nil
}

func runAdd(s *store.Store, args []string) error {
	var projectID, title, body string
	var priority int

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p":
			if i+1 < len(args) {
				projectID = args[i+1]
				i++
			}
		case "-m":
			if i+1 < len(args) {
				body = args[i+1]
				i++
			}
		default:
			title = args[i]
		}
	}

	if title == "" {
		return fmt.Errorf("title required")
	}
	if projectID == "" {
		return fmt.Errorf("project id required (use -p)")
	}

	t, err := s.CreateTask(projectID, title, body, priority)
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runDone(s *store.Store, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: segments done <project-id> <task-id>")
	}
	t, err := s.UpdateTask(args[0], args[1], "", "", models.StatusDone, 0, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runUpdate(s *store.Store, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: segments update <project-id> <task-id> <title>")
	}
	t, err := s.UpdateTask(args[0], args[1], args[2], "", models.StatusTodo, 0, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runRm(s *store.Store, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: segments rm <project-id> <task-id>")
	}
	if err := s.DeleteTask(args[0], args[1]); err != nil {
		return err
	}
	notifyServer()
	return nil
}

func runStatus(s *store.Store) error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	fmt.Printf("Projects: %d\n", len(projects))
	for _, p := range projects {
		tasks, err := s.ListTasks(p.ID)
		if err != nil {
			continue
		}
		var done int
		for _, t := range tasks {
			if t.Status == models.StatusDone {
				done++
			}
		}
		fmt.Printf("  %s: %d/%d tasks\n", p.Name, done, len(tasks))
	}
	return nil
}

func runMCP(s *store.Store) error {
	return mcpServer(s)
}

func mcpServer(s *store.Store) error {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req map[string]interface{}
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		resp := handleMCP(s, req)
		enc.Encode(resp)
	}
}

func handleMCP(s *store.Store, req map[string]interface{}) map[string]interface{} {
	method, _ := req["method"].(string)
	id, _ := req["id"]

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":     id,
	}

	switch method {
	case "initialize":
		resp["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo":     map[string]string{"name": "segments", "version": "0.1.0"},
		}
	case "tools/list":
		resp["result"] = map[string]interface{}{
			"tools": []map[string]interface{}{
				{"name": "segments_list_projects", "description": "List all projects"},
				{"name": "segments_create_project", "description": "Create a project"},
				{"name": "segments_list_tasks", "description": "List tasks for a project"},
				{"name": "segments_create_task", "description": "Create a task"},
				{"name": "segments_update_task", "description": "Update a task"},
				{"name": "segments_delete_task", "description": "Delete a task"},
				{"name": "segments_get_task", "description": "Get a task"},
			},
		}
	case "tools/call":
		tool, _ := req["params"].(map[string]interface{})["name"].(string)
		args, _ := req["params"].(map[string]interface{})["arguments"].(map[string]interface{})
		result := callTool(s, tool, args)
		resp["result"] = map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": result}},
		}
	default:
		resp["error"] = map[string]string{"code": "-32601", "message": "method not found"}
	}

	return resp
}

func callTool(s *store.Store, name string, args map[string]interface{}) string {
	switch name {
	case "segments_list_projects":
		list, _ := s.ListProjects()
		data, _ := json.Marshal(list)
		return string(data)
	case "segments_create_project":
		name, _ := args["name"].(string)
		p, _ := s.CreateProject(name)
		data, _ := json.Marshal(p)
		notifyServer()
		return string(data)
	case "segments_list_tasks":
		projectID, _ := args["project_id"].(string)
		list, _ := s.ListTasks(projectID)
		data, _ := json.Marshal(list)
		return string(data)
	case "segments_create_task":
		projectID, _ := args["project_id"].(string)
		title, _ := args["title"].(string)
		t, _ := s.CreateTask(projectID, title, "", 0)
		data, _ := json.Marshal(t)
		notifyServer()
		return string(data)
	case "segments_update_task":
		projectID, _ := args["project_id"].(string)
		taskID, _ := args["task_id"].(string)
		title, _ := args["title"].(string)
		t, _ := s.UpdateTask(projectID, taskID, title, "", models.StatusTodo, 0, "")
		data, _ := json.Marshal(t)
		notifyServer()
		return string(data)
	case "segments_delete_task":
		projectID, _ := args["project_id"].(string)
		taskID, _ := args["task_id"].(string)
		s.DeleteTask(projectID, taskID)
		notifyServer()
		return `{"deleted": true}`
	case "segments_get_task":
		projectID, _ := args["project_id"].(string)
		taskID, _ := args["task_id"].(string)
		t, _ := s.GetTask(projectID, taskID)
		data, _ := json.Marshal(t)
		return string(data)
	default:
		return `{"error": "unknown tool"}`
	}
}

func expandPath(path string) string {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(expanded, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, expanded[1:])
	}
	return expanded
}