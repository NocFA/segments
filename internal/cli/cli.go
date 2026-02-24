package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"


	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/server"
	"codeberg.org/nocfa/segments/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

var (
	cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9eff"))
	green   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24"))
	red     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	dim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))
	bold    = lipgloss.NewStyle().Bold(true)
	box     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#333")).Padding(1, 2)
)

var dataDir = func() string {
	if d := os.Getenv("SEGMENTS_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".segments")
}()

var pidFile = filepath.Join(dataDir, "pid")

func getPID() int {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 1 {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
	return pid
}

func isRunning() bool {
	pid := getPID()
	if pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Pid == pid
}

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
		return fmt.Errorf("usage: segments <command>\nvalid commands:\n  sg init, sg start, sg stop, sg list, sg add, sg done\n  sg uninstall (or: segments setup)")
	}

	cmd := args[1]

	// Handle sg prefix: 'sg start' -> 'segments serve'
	// Also handle when called as alias: 'segments start' -> 'segments serve'
	if cmd == "sg" || (len(args) >= 2 && (args[1] == "start" || args[1] == "stop" || args[1] == "list" || args[1] == "uninstall")) {
		if len(args) < 2 {
			fmt.Println("sg: usage: sg <command>")
			fmt.Println("sg: valid commands: start, stop, list, uninstall, add, done, update, rm, tasks, beads")
			return nil
		}
		var subCmd string
		switch args[1] {
		case "start":
			subCmd = "serve"
		case "stop":
			subCmd = "stop"
		case "uninstall", "remove":
			subCmd = "remove"
		case "status", "list":
			subCmd = "list"
		case "add", "done", "update", "rm", "tasks", "beads":
			subCmd = args[1]
		default:
			fmt.Fprintf(os.Stderr, "sg: unknown command %s\n", args[1])
			fmt.Fprintf(os.Stderr, "valid: start, stop, list, uninstall, add, done, update, rm, tasks, beads\n")
			return nil
		}
		// Rebuild args: [segments, subCmd, rest...]
		newArgs := make([]string, 0, len(args))
		newArgs = append(newArgs, args[0])
		newArgs = append(newArgs, subCmd)
		newArgs = append(newArgs, args[2:]...)
		args = newArgs
		cmd = subCmd
	}

	s := store.NewStore(expandPath(dataDir))

	switch cmd {
	case "serve", "start":
		return runServe(s)
	case "stop":
		return runStop()
	case "init":
		return runInit(s)
	case "list", "status":
		return runList(s, args[2:])
	case "add":
		return runAdd(s, args[2:])
	case "done":
		return runDone(s, args[2:])
	case "beads":
		return runBeads(s, args[2:])
	case "setup":
		return runSetup()
	case "shell":
		return runShell()
	case "remove", "uninstall":
		return runRemove()
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func runServe(s *store.Store) error {
	if os.Getenv("SEGMENTS_DAEMON") == "1" {
		return runServeDaemon(s)
	}

	_, err := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
	}

	if isRunning() {
		return fmt.Errorf("segments is already running (pid: %d)", getPID())
	}

	// Auto-detect integrations in current project
	autoDetectIntegrations()

	cmd := exec.Command(os.Args[0], "serve")
	cmd.Env = append(os.Environ(), "SEGMENTS_DAEMON=1")
	home, _ := os.UserHomeDir()
	logFile := filepath.Join(home, ".segments", "daemon.log")
	if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	cmd.Start()
	fmt.Println()
	fmt.Println(bold.Render("Segments started ") + green.Render("(pid: "+fmt.Sprintf("%d", cmd.Process.Pid)+")"))
	fmt.Println(bold.Render("Run: ") + cyan.Render("sg list") + dim.Render(" | sg shell"))
	return nil
}

func autoDetectIntegrations() {
	cwd, _ := os.Getwd()
	var detected []string

	piExt := filepath.Join(cwd, ".pi", "extensions")
	if _, err := os.Stat(piExt); err == nil {
		if files, err := os.ReadDir(piExt); err == nil {
			for _, f := range files {
				if strings.HasPrefix(f.Name(), "segments") {
					detected = append(detected, "  → Pi: "+cyan.Render(f.Name()))
					break
				}
			}
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, "opencode.json")); err == nil {
		detected = append(detected, "  → OpenCode MCP")
	}
	beadsDir := filepath.Join(cwd, ".beads")
	if _, err := os.Stat(beadsDir); err == nil {
		if _, err := os.Stat(filepath.Join(beadsDir, "issues.jsonl")); err == nil {
			detected = append(detected, "  → Beads")
		}
	}

	if len(detected) > 0 {
		fmt.Println()
		for _, d := range detected {
			fmt.Printf("%s\n", d)
		}
		fmt.Println()
		fmt.Println(dim.Render("Run: ") + cyan.Render("sg setup") + dim.Render("  to configure"))
		fmt.Println(dim.Render("  or: ") + cyan.Render("sg beads") + dim.Render("  to import"))
	}
}

func runServeDaemon(s *store.Store) error {
	cfg, err := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
	}

	dir := server.ExpandPath(cfg.DataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	s = store.NewStore(dir)
	hub := server.NewHub()
	srv := server.NewServer(s, hub, cfg, pidFile)

	if cfg.Extension != "" {
		fmt.Printf("Auto-loaded extension: %s\n", cfg.Extension)
	}
	if cfg.EnableMCP {
		fmt.Println("MCP: enabled")
	}

	fmt.Println("Starting Segments server...")
	return srv.Start()
}

func runStop() error {
	if !isRunning() {
		return fmt.Errorf("segments is not running")
	}

	pid := getPID()
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("signal: %w", err)
	}
	fmt.Println()
	fmt.Println(bold.Render("Segments stopped ") + red.Render("(pid: "+fmt.Sprintf("%d", pid)+")"))
	return nil
}

func runList(s *store.Store, args []string) error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}

	if len(projects) == 0 {
		fmt.Println("No projects. Run 'segments projects add <name>' to create one.")
		return nil
	}

	fmt.Println()
	fmt.Println(bold.Render("Projects: ") + cyan.Render(fmt.Sprintf("%d", len(projects))))
	for _, p := range projects {
		tasks, _ := s.ListTasks(p.ID)
		var done int
		for _, t := range tasks {
			if t.Status == models.StatusDone {
				done++
			}
		}
		var st string
		if done == len(tasks) {
			st = green.Render(fmt.Sprintf("%d/%d", done, len(tasks))) + dim.Render(" done")
		} else {
			st = yellow.Render(fmt.Sprintf("%d/%d", done, len(tasks))) + dim.Render(" done")
		}
		fmt.Printf("  %s %s (%s)\n", cyan.Render(p.ID[:8]), bold.Render(p.Name), st)
	}
	return nil
}

func runRemove() error {
	var force bool
	for _, arg := range os.Args[1:] {
		if arg == "-f" || arg == "--force" {
			force = true
		}
	}

	if !force {
		// Try TUI, don't fall back if it errors
		conf := runRemoveTUI()
		if !conf { return }
	}

	return runRemoveImpl()
}


func runRemoveImpl() error {
	// Stop server if running
	if isRunning() {
		pid := getPID()
		proc, _ := os.FindProcess(pid)
		if proc != nil {
			proc.Signal(os.Interrupt)
		}
	}

	home, _ := os.UserHomeDir()
	os.RemoveAll(filepath.Join(home, ".segments"))

	// Remove executable
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", "segments"),
		"/usr/local/bin/segments",
		"/usr/bin/segments",
	} {
		os.Remove(p)
	}

	// Remove shell binding
	for _, rc := range []string{".zshrc", ".bashrc"} {
		path := filepath.Join(home, rc)
		if data, err := os.ReadFile(path); err == nil {
			lines := strings.Split(string(data), "\n")
			var kept []string
			for _, line := range lines {
				if strings.Contains(line, "# segments") || strings.Contains(line, "sg() { segments") {
					continue
				}
				kept = append(kept, line)
			}
			os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0644)
		}
	}

	fmt.Println("Segments removed completely.")
	return nil
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

func runBeads(s *store.Store, args []string) error {
	var beadsDir string
	var projectName string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d":
			if i+1 < len(args) {
				beadsDir = args[i+1]
				i++
			}
		case "-p":
			if i+1 < len(args) {
				projectName = args[i+1]
				i++
			}
		}
	}

	if beadsDir == "" {
		home, _ := os.UserHomeDir()
		beadsDir = filepath.Join(home, "Dev", "segments", ".beads")
	}

	issuesFile := filepath.Join(beadsDir, "issues.jsonl")
	data, err := os.ReadFile(issuesFile)
	if err != nil {
		return fmt.Errorf("read issues.jsonl: %w", err)
	}

	var proj *models.Project
	if projectName == "" {
		projectName = "Beads Import"
	}
	proj, err = s.CreateProject(projectName)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	fmt.Printf("Created project: %s %s\n", proj.ID, proj.Name)

	var imported, skipped int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}

		var bead struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Status      string   `json:"status"`
			Priority    int      `json:"priority"`
			IssueType   string   `json:"issue_type"`
			Labels      []string `json:"labels"`
			CreatedAt   string   `json:"created_at"`
			ClosedAt    string   `json:"closed_at"`
			CloseReason string   `json:"close_reason"`
		}
		if err := json.Unmarshal([]byte(line), &bead); err != nil {
			skipped++
			continue
		}

		if bead.IssueType != "task" {
			skipped++
			continue
		}

		body := bead.Description
		if bead.CloseReason != "" {
			body += "\n\n---\nClosed: " + bead.CloseReason
		}
		if len(bead.Labels) > 0 {
			body += "\n\nLabels: " + strings.Join(bead.Labels, ", ")
		}
		body += "\n\n[Imported from bead: " + bead.ID + "]"

		var status models.TaskStatus
		switch bead.Status {
		case "open", "":
			status = models.StatusTodo
		case "in_progress":
			status = models.StatusInProgress
		case "closed":
			status = models.StatusDone
		case "blocker":
			status = models.StatusBlocker
		default:
			status = models.StatusTodo
		}

		_, err = s.CreateTask(proj.ID, bead.Title, body, bead.Priority)
		if err != nil {
			skipped++
			continue
		}

		if status == models.StatusDone {
			tasks, _ := s.ListTasks(proj.ID)
			if len(tasks) > 0 {
				last := tasks[len(tasks)-1]
				s.UpdateTask(proj.ID, last.ID, "", "", models.StatusDone, 0, "")
			}
		}

		imported++
	}


	fmt.Println(bold.Render("Imported ") + green.Render(fmt.Sprintf("%d", imported)) + dim.Render(" tasks (") + yellow.Render(fmt.Sprintf("%d", skipped)) + dim.Render(" skipped)"))
	notifyServer()
	return nil
}

func runShell() error {
	fmt.Println()
	fmt.Println(bold.Render("# Add sg alias to .zshrc or .bashrc:"))
	fmt.Println(dim.Render("sg() { segments \"$@\"; }"))
	fmt.Println()
	fmt.Println(bold.Render("# Commands: ") + cyan.Render("sg start") + dim.Render(" | ") + cyan.Render("sg stop") + dim.Render(" | ") + cyan.Render("sg list") + dim.Render(" | ") + cyan.Render("sg uninstall"))
	return nil
}

func runSetup() error {
	cwd, _ := os.Getwd()
	fmt.Println()
	fmt.Println(bold.Render("=== Segments Setup ==="))
	fmt.Println("")

	var beads, mcp, pi bool

	if _, err := os.Stat(filepath.Join(cwd, ".beads", "issues.jsonl")); err == nil {
		fmt.Println(" " + green.Render("✓") + " Beads: " + dim.Render(".beads/issues.jsonl") + yellow.Render(" (import pending)"))
		beads = true
	}
	if _, err := os.Stat(filepath.Join(cwd, "opencode.json")); err == nil {
		fmt.Println(" " + green.Render("✓") + " OpenCode: " + dim.Render("opencode.json") + yellow.Render(" (MCP pending)"))
		mcp = true
	}
	if _, err := os.Stat(filepath.Join(cwd, ".pi", "extensions")); err == nil {
		fmt.Println(" " + green.Render("✓") + " Pi: " + dim.Render(".pi/extensions") + green.Render(" (loaded)"))
		pi = true
	}
	if !beads && !mcp && !pi {
		fmt.Println("No integrations detected. Run 'sg start' first.")
	}
	fmt.Println("\nServer running at http://localhost:8765")
	fmt.Println("Run 'sg list' to see projects.")
	return nil
}

func expandPath(path string) string {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(expanded, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, expanded[1:])
	}
	return expanded
}