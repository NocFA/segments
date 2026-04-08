package cli

import (
	"bytes"
	_ "embed"
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

//go:embed segments.ts
var piExtensionTS string

var (
	cyan  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9eff"))
	green = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24"))
	red   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	dim   = lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))
	bold  = lipgloss.NewStyle().Bold(true)
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#333")).Padding(1, 2)
)

var dataDir = func() string {
	if d := os.Getenv("SEGMENTS_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".segments")
}()

var pidFile = filepath.Join(dataDir, "pid")

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

func getPID() int {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]))
	return pid
}

func isRunning() bool {
	pid := getPID()
	if pid == 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Pid == pid
}

func pidFileData() (int, string, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, "", err
	}
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("invalid pid file")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", err
	}
	return pid, strings.TrimSpace(lines[1]), nil
}

func notifyServer() {
	pid, addr, err := pidFileData()
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pid); err != nil || p.Pid != pid {
		return
	}
	// addr is either "host:port" or just a port (legacy pid files)
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}
	http.Post("http://"+addr+"/internal/sync", "application/json", bytes.NewReader(nil))
}

// aliases maps user-facing command names to internal ones.
var aliases = map[string]string{
	"start":     "serve",
	"stop":      "stop",
	"uninstall": "remove",
	"remove":    "remove",
	"list":      "list",
	"status":    "list",
	"install":   "setup",
}

func Run(args []string, version string) error {
	if len(args) < 2 {
		fmt.Println("usage: segments <command>")
		fmt.Println("  start, stop, list, add, done, rename, setup, init, shell, uninstall")
		return nil
	}

	cmd := args[1]
	rest := args[2:]

	if mapped, ok := aliases[cmd]; ok {
		cmd = mapped
	}

	s := store.NewStore(expandPath(dataDir))

	switch cmd {
	case "serve":
		return runServe(s)
	case "stop":
		return runStop()
	case "init":
		return runInit(s)
	case "list":
		return runList(s, rest)
	case "add":
		return runAdd(s, rest)
	case "done":
		return runDone(s, rest)
	case "close":
		return runClose(s, rest)
	case "rename":
		return runRename(s, rest)
	case "beads":
		return runBeads(s, rest)
	case "setup":
		return runSetup(s)
	case "shell":
		return runShell()
	case "remove":
		return runRemove()
	case "version":
		fmt.Println(version)
		return nil
	case "mcp":
		return mcpServer(s)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func runServe(s *store.Store) error {
	if os.Getenv("SEGMENTS_DAEMON") == "1" {
		return runServeDaemon(s)
	}

	if isRunning() {
		return fmt.Errorf("already running (pid: %d)", getPID())
	}

	cfg, _ := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	autoDetectIntegrations()

	cmd := exec.Command(os.Args[0], "serve")
	cmd.Env = append(os.Environ(), "SEGMENTS_DAEMON=1")
	logPath := filepath.Join(dataDir, "daemon.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	listenAddr := cfg.Bind + ":" + cfg.Port
	fmt.Println()
	fmt.Println(bold.Render("Segments started ") + green.Render("(pid: "+strconv.Itoa(cmd.Process.Pid)+")"))
	fmt.Println(dim.Render("  Listening: ") + cyan.Render("http://"+listenAddr))
	fmt.Println(bold.Render("Run: ") + cyan.Render("sg list") + dim.Render(" | sg shell"))
	fmt.Println()
	return nil
}

func autoDetectIntegrations() {
	cwd, _ := os.Getwd()
	var found []string

	// Pi: check if 'pi' command is available in PATH
	if piPath := findInPath("pi"); piPath != "" {
		found = append(found, "  Pi: "+cyan.Render(piPath))
	}

	// OpenCode: check if 'opencode' command is available in PATH
	if opencodePath := findInPath("opencode"); opencodePath != "" {
		found = append(found, "  OpenCode: "+cyan.Render(opencodePath))
	}
	if _, err := os.Stat(filepath.Join(cwd, ".beads", "issues.jsonl")); err == nil {
		found = append(found, "  Beads")
	}

	if len(found) == 0 {
		return
	}

	fmt.Println()
	for _, f := range found {
		fmt.Println(f)
	}
	fmt.Println()
	fmt.Println(dim.Render("Run: ") + cyan.Render("sg setup") + dim.Render("  to configure"))
	fmt.Println(dim.Render("  or: ") + cyan.Render("sg beads") + dim.Render("  to import"))
}

func runServeDaemon(s *store.Store) error {
	cfg, err := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
	}

	dir := server.ExpandPath(cfg.DataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	s = store.NewStore(dir)
	hub := server.NewHub()
	srv := server.NewServer(s, hub, cfg, pidFile)

	if cfg.Extension != "" {
		fmt.Printf("Extension: %s\n", cfg.Extension)
	}
	if cfg.EnableMCP {
		fmt.Println("MCP: enabled")
	}

	return srv.Start()
}

func runStop() error {
	if !isRunning() {
		return fmt.Errorf("not running")
	}

	pid := getPID()
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(bold.Render("Segments stopped ") + red.Render("(pid: "+strconv.Itoa(pid)+")"))
	fmt.Println()
	return nil
}

func runList(s *store.Store, args []string) error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}

	if len(projects) == 0 {
		fmt.Println("No projects yet.")
		return nil
	}

	fmt.Println()
	fmt.Println(bold.Render("Projects: ") + cyan.Render(strconv.Itoa(len(projects))))
	for _, p := range projects {
		tasks, _ := s.ListTasks(p.ID)
		var done int
		for _, t := range tasks {
			if t.Status == models.StatusDone {
				done++
			}
		}
		progress := fmt.Sprintf("%d/%d", done, len(tasks))
		color := yellow
		if done == len(tasks) {
			color = green
		}
		fmt.Printf("  %s %s (%s%s)\n", cyan.Render(p.ID[:8]), bold.Render(p.Name), color.Render(progress), dim.Render(" done"))
	}
	fmt.Println()
	return nil
}

func runRemove() error {
	for _, arg := range os.Args[1:] {
		if arg == "-f" || arg == "--force" {
			return doRemove()
		}
	}

	if !confirm("Uninstall Segments?", "Removes all projects, tasks, server, and shell alias.") {
		fmt.Println("Cancelled.")
		return nil
	}
	return doRemove()
}

func doRemove() error {
	if isRunning() {
		if proc, err := os.FindProcess(getPID()); err == nil {
			proc.Signal(os.Interrupt)
		}
	}

	home, _ := os.UserHomeDir()
	os.RemoveAll(filepath.Join(home, ".segments"))

	for _, p := range []string{"segments", "sg"} {
		os.Remove(filepath.Join(home, ".local", "bin", p))
	}

	for _, rc := range []string{".zshrc", ".bashrc"} {
		path := filepath.Join(home, rc)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var kept []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "# segments") {
				continue
			}
			kept = append(kept, line)
		}
		os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0644)
	}

	cwd, _ := os.Getwd()
	// Clean local integrations
	os.Remove(filepath.Join(cwd, ".pi", "extensions", "segments.ts"))
	removeOpenCodeMCP(cwd)
	removeMCPEntry(filepath.Join(cwd, ".mcp.json"))
	// Clean global integrations
	os.Remove(filepath.Join(home, ".pi", "agent", "extensions", "segments.ts"))
	removeMCPEntry(filepath.Join(home, ".claude", "mcp.json"))

	fmt.Println("Segments removed.")
	fmt.Println(dim.Render("Run: ") + cyan.Render("hash -r") + dim.Render(" to clear shell cache"))
	fmt.Println()
	return nil
}

func ensureDataDir() error {
	if err := os.MkdirAll(expandPath(dataDir), 0755); err != nil {
		return err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if !fileExists(cfgPath) {
		yamlData := []byte("port: \"8765\"\nbind: \"127.0.0.1\"\ndata_dir: \"~/.segments\"\n")
		return os.WriteFile(cfgPath, yamlData, 0644)
	}
	return nil
}

// runInit sets up local (per-project) integrations in the current directory.
// When called non-interactively (e.g. from install script), it only ensures
// the data directory and config exist.
func runInit(s *store.Store) error {
	if err := ensureDataDir(); err != nil {
		return err
	}

	// Non-interactive: just ensure data dir exists (install script calls this)
	if !isTerminal() {
		return nil
	}

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	bin := filepath.Join(home, ".local", "bin", "segments")

	fmt.Println()
	fmt.Println(bold.Render("Segments Init") + dim.Render(" (local project setup)"))
	fmt.Println(dim.Render("  Directory: ") + cyan.Render(cwd))
	fmt.Println()

	setupIntegrations(s, scopeLocal, cwd, home, bin)

	fmt.Println()
	return nil
}

func runAdd(s *store.Store, args []string) error {
	var projectID, title, body string

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
		return fmt.Errorf("project id required (-p)")
	}

	t, err := s.CreateTask(projectID, title, body, 0)
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runClose(s *store.Store, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sg close <project-id> <task-id>")
	}
	t, err := s.UpdateTask(args[0], args[1], "", "", models.StatusClosed, 0, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runRename(s *store.Store, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sg rename <project-id> <new-name>")
	}
	p, err := s.UpdateProject(args[0], strings.Join(args[1:], " "))
	if err != nil {
		return err
	}
	fmt.Println(p.ID + " " + p.Name)
	notifyServer()
	return nil
}

func runDone(s *store.Store, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sg done <project-id> <task-id>")
	}
	t, err := s.UpdateTask(args[0], args[1], "", "", models.StatusDone, 0, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServer()
	return nil
}

func runBeads(s *store.Store, args []string) error {
	var beadsDir, projectName string

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
		cwd, _ := os.Getwd()
		beadsDir = filepath.Join(cwd, ".beads")
	}
	if projectName == "" {
		projectName = filepath.Base(filepath.Dir(beadsDir))
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "issues.jsonl"))
	if err != nil {
		return fmt.Errorf("read issues.jsonl: %w", err)
	}

	proj, err := s.CreateProject(projectName)
	if err != nil {
		return err
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

		_, err = s.CreateTask(proj.ID, bead.Title, body, bead.Priority)
		if err != nil {
			skipped++
			continue
		}

		if bead.Status == "closed" {
			tasks, _ := s.ListTasks(proj.ID)
			if len(tasks) > 0 {
				last := tasks[len(tasks)-1]
				s.UpdateTask(proj.ID, last.ID, "", "", models.StatusClosed, 0, "")
			}
		}

		imported++
	}

	fmt.Println(bold.Render("Imported ") + green.Render(strconv.Itoa(imported)) + dim.Render(" tasks (") + yellow.Render(strconv.Itoa(skipped)) + dim.Render(" skipped)"))
	notifyServer()
	return nil
}

func runShell() error {
	fmt.Println()
	fmt.Println(bold.Render("Commands"))
	fmt.Println("  " + cyan.Render("sg start") + dim.Render("      start the server"))
	fmt.Println("  " + cyan.Render("sg stop") + dim.Render("       stop the server"))
	fmt.Println("  " + cyan.Render("sg list") + dim.Render("       list projects"))
	fmt.Println("  " + cyan.Render("sg setup") + dim.Render("      configure integrations (global/local)"))
	fmt.Println("  " + cyan.Render("sg init") + dim.Render("       add integrations to current project"))
	fmt.Println("  " + cyan.Render("sg uninstall") + dim.Render("  remove everything"))
	fmt.Println()
	return nil
}

type installScope string

const (
	scopeGlobal installScope = "global"
	scopeLocal  installScope = "local"
)

type integration struct {
	name    string
	scope   installScope // which scope this integration applies to
	detect  func() bool
	path    func() string // returns the target file path
	content func() string // returns expected content (empty for non-file integrations)
	setup   func() error
	prompt  string
	detail  string
}

// integrationStatus returns the status of an integration:
// "missing" - not installed, "current" - installed and up to date, "outdated" - installed but content differs
func integrationStatus(ig integration) string {
	p := ig.path()
	if p == "" || !fileExists(p) {
		return "missing"
	}
	expected := ig.content()
	if expected == "" {
		// MCP configs and non-file integrations: check if segments key exists in JSON
		if strings.HasSuffix(p, ".json") {
			if mcpConfigured(p) {
				return "current"
			}
			return "missing"
		}
		// Non-JSON (e.g. beads) - file exists means configured
		return "current"
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "missing"
	}
	if string(data) == expected {
		return "current"
	}
	return "outdated"
}

func buildIntegrations(s *store.Store, scope installScope, cwd, home, bin string) []integration {
	var igs []integration

	// Pi extension
	if findInPath("pi") != "" {
		var piDir string
		if scope == scopeGlobal {
			piDir = filepath.Join(home, ".pi", "agent", "extensions")
		} else {
			piDir = filepath.Join(cwd, ".pi", "extensions")
		}
		piPath := filepath.Join(piDir, "segments.ts")
		igs = append(igs, integration{
			name:    "Pi",
			scope:   scope,
			detect:  func() bool { return true },
			path:    func() string { return piPath },
			content: func() string { return piExtensionTS },
			setup: func() error {
				os.MkdirAll(piDir, 0755)
				return os.WriteFile(piPath, []byte(piExtensionTS), 0644)
			},
			prompt: "Set up Pi extension?",
			detail: fmt.Sprintf("Writes segments.ts to %s", piDir),
		})
	}

	// Claude Code MCP
	if _, err := exec.LookPath("claude"); err == nil {
		var mcpPath string
		if scope == scopeGlobal {
			mcpPath = filepath.Join(home, ".claude", "mcp.json")
		} else {
			mcpPath = filepath.Join(cwd, ".mcp.json")
		}
		igs = append(igs, integration{
			name:   "Claude Code",
			scope:  scope,
			detect: func() bool { return true },
			path:   func() string { return mcpPath },
			content: func() string {
				// MCP config is JSON-merged, just check if key exists
				return ""
			},
			setup:  func() error { return writeMCPConfig(mcpPath, bin) },
			prompt: "Set up Claude Code MCP?",
			detail: fmt.Sprintf("Adds segments to %s", mcpPath),
		})
	}

	// OpenCode MCP
	if findInPath("opencode") != "" {
		var ocPath string
		if scope == scopeLocal {
			ocPath = filepath.Join(cwd, "opencode.json")
		} else {
			// Check known global locations
			for _, p := range []string{
				filepath.Join(home, ".opencode", "opencode.json"),
				filepath.Join(home, "Library", "Application Support", "opencode", "opencode.json"),
			} {
				if fileExists(p) {
					ocPath = p
					break
				}
			}
			if ocPath == "" {
				ocPath = filepath.Join(home, ".opencode", "opencode.json")
			}
		}
		igs = append(igs, integration{
			name:    "OpenCode",
			scope:   scope,
			detect:  func() bool { return true },
			path:    func() string { return ocPath },
			content: func() string { return "" },
			setup: func() error {
				os.MkdirAll(filepath.Dir(ocPath), 0755)
				return writeMCPConfig(ocPath, bin)
			},
			prompt: "Set up OpenCode MCP?",
			detail: fmt.Sprintf("Adds segments to %s", ocPath),
		})
	}

	// Beads import (only for local scope, and only if .beads exists)
	if scope == scopeLocal && fileExists(filepath.Join(cwd, ".beads", "issues.jsonl")) {
		igs = append(igs, integration{
			name:   "Beads",
			scope:  scopeLocal,
			detect: func() bool { return true },
			path: func() string {
				// Check if we already imported by looking for a project
				// named after this directory
				projects, _ := s.ListProjects()
				dirName := filepath.Base(cwd)
				for _, p := range projects {
					if p.Name == dirName {
						return filepath.Join(cwd, ".beads", "issues.jsonl") // exists = "current"
					}
				}
				return "" // empty = "missing"
			},
			content: func() string { return "" },
			setup:  func() error { return runBeads(s, nil) },
			prompt: "Import Beads issues?",
			detail: "Creates a project with tasks from .beads/issues.jsonl",
		})
	}

	return igs
}

func setupIntegrations(s *store.Store, scope installScope, cwd, home, bin string) {
	igs := buildIntegrations(s, scope, cwd, home, bin)

	any := false
	for _, ig := range igs {
		if !ig.detect() {
			continue
		}
		any = true

		status := integrationStatus(ig)
		switch status {
		case "current":
			fmt.Println("  " + green.Render("✓") + " " + ig.name + green.Render(" (up to date)"))
			continue
		case "outdated":
			fmt.Println("  " + yellow.Render("↑") + " " + ig.name + yellow.Render(" (update available)"))
			if confirm("Update existing "+ig.name+" integration?", ig.detail) {
				if err := ig.setup(); err != nil {
					fmt.Println("    " + red.Render(err.Error()))
				} else {
					fmt.Println("    " + green.Render("Updated."))
				}
			}
			fmt.Println()
		case "missing":
			fmt.Println("  " + yellow.Render("○") + " " + ig.name)
			if confirm(ig.prompt, ig.detail) {
				if err := ig.setup(); err != nil {
					fmt.Println("    " + red.Render(err.Error()))
				} else {
					fmt.Println("    " + green.Render("Done."))
				}
			}
			fmt.Println()
		}
	}

	if !any {
		fmt.Println("  No supported tools detected.")
		fmt.Println("  Supports: Pi, Claude Code, OpenCode")
	}
}

func runSetup(s *store.Store) error {
	if err := ensureDataDir(); err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	bin := filepath.Join(home, ".local", "bin", "segments")

	fmt.Println()
	fmt.Println(bold.Render("Segments Setup"))
	fmt.Println()

	// Prompt for scope
	idx := selectOption(
		"Where should integrations be installed?",
		[]string{"Global", "Local"},
		[]string{"Machine-wide, all projects can use segments", "Only in current project directory"},
	)
	if idx == -1 {
		fmt.Println("Cancelled.")
		return nil
	}

	scope := scopeGlobal
	if idx == 1 {
		scope = scopeLocal
	}

	fmt.Println()
	if scope == scopeGlobal {
		fmt.Println(dim.Render("  Scope: ") + cyan.Render("global"))
	} else {
		fmt.Println(dim.Render("  Scope: ") + cyan.Render("local") + dim.Render(" ("+cwd+")"))
	}
	fmt.Println()

	setupIntegrations(s, scope, cwd, home, bin)

	cfg, _ := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	listenAddr := cfg.Bind + ":" + cfg.Port

	fmt.Println()
	fmt.Println(dim.Render("Server: ") + cyan.Render("http://"+listenAddr))
	fmt.Println(dim.Render("Tip: ") + cyan.Render("sg init") + dim.Render(" to add integrations to any project directory"))
	fmt.Println()
	return nil
}

func mcpEntry(bin string) map[string]interface{} {
	return map[string]interface{}{
		"command": bin,
		"args":    []string{"mcp"},
	}
}

func mcpConfigured(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	servers, _ := cfg["mcpServers"].(map[string]interface{})
	_, ok := servers["segments"]
	return ok
}

func writeMCPConfig(path, bin string) error {
	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cfg)
	}
	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers["segments"] = mcpEntry(bin)
	cfg["mcpServers"] = servers
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}

func setupOpenCodeMCP(cwd, home, bin string) error {
	// Try known opencode config locations
	var path string
	for _, p := range []string{
		filepath.Join(cwd, "opencode.json"),
		filepath.Join(home, ".opencode", "opencode.json"),
		filepath.Join(home, ".opencode", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "opencode", "opencode.json"),
	} {
		if fileExists(p) {
			path = p
			break
		}
	}
	if path == "" {
		path = filepath.Join(home, ".opencode", "opencode.json")
	}

	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cfg)
	}

	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers["segments"] = mcpEntry(bin)
	cfg["mcpServers"] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, append(out, '\n'), 0644)
}

func removeMCPEntry(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if servers == nil {
		return
	}
	delete(servers, "segments")
	if len(servers) == 0 {
		delete(cfg, "mcpServers")
	} else {
		cfg["mcpServers"] = servers
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, append(out, '\n'), 0644)
}

func removeOpenCodeMCP(cwd string) {
	home, _ := os.UserHomeDir()
	removeMCPEntry(filepath.Join(cwd, "opencode.json"))
	removeMCPEntry(filepath.Join(home, "Library", "Application Support", "opencode", "opencode.json"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
		enc.Encode(handleMCP(s, req))
	}
}

func handleMCP(s *store.Store, req map[string]interface{}) map[string]interface{} {
	method, _ := req["method"].(string)
	id := req["id"]

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	switch method {
	case "initialize":
		resp["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo":      map[string]string{"name": "segments", "version": "0.1.0"},
		}
	case "tools/list":
		resp["result"] = map[string]interface{}{
			"tools": []map[string]interface{}{
				{"name": "segments_list_projects", "description": "List all projects"},
				{"name": "segments_create_project", "description": "Create a project"},
				{"name": "segments_rename_project", "description": "Rename a project"},
				{"name": "segments_list_tasks", "description": "List tasks for a project"},
				{"name": "segments_create_task", "description": "Create a task"},
				{"name": "segments_update_task", "description": "Update a task"},
				{"name": "segments_delete_task", "description": "Delete a task"},
				{"name": "segments_get_task", "description": "Get a task"},
			},
		}
	case "tools/call":
		params, _ := req["params"].(map[string]interface{})
		tool, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		resp["result"] = map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": callTool(s, tool, args)}},
		}
	default:
		resp["error"] = map[string]string{"code": "-32601", "message": "method not found"}
	}

	return resp
}

func callTool(s *store.Store, tool string, args map[string]interface{}) string {
	str := func(key string) string { v, _ := args[key].(string); return v }
	marshal := func(v interface{}) string { d, _ := json.Marshal(v); return string(d) }

	switch tool {
	case "segments_list_projects":
		list, _ := s.ListProjects()
		return marshal(list)
	case "segments_create_project":
		p, _ := s.CreateProject(str("name"))
		notifyServer()
		return marshal(p)
	case "segments_rename_project":
		p, _ := s.UpdateProject(str("project_id"), str("name"))
		notifyServer()
		return marshal(p)
	case "segments_list_tasks":
		list, _ := s.ListTasks(str("project_id"))
		return marshal(list)
	case "segments_create_task":
		t, _ := s.CreateTask(str("project_id"), str("title"), "", 0)
		notifyServer()
		return marshal(t)
	case "segments_update_task":
		t, _ := s.UpdateTask(str("project_id"), str("task_id"), str("title"), "", models.StatusTodo, 0, "")
		notifyServer()
		return marshal(t)
	case "segments_delete_task":
		s.DeleteTask(str("project_id"), str("task_id"))
		notifyServer()
		return `{"deleted": true}`
	case "segments_get_task":
		t, _ := s.GetTask(str("project_id"), str("task_id"))
		return marshal(t)
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

// findInPath searches PATH for a command, returns its absolute path or "".
func findInPath(cmd string) string {
	// Try LookPath first — finds it anywhere in PATH
	if path, err := exec.LookPath(cmd); err == nil {
		return path
	}

	// Fallback: check common install locations directly
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".opencode", "bin", cmd),
		filepath.Join(home, ".local", "bin", cmd),
		"/opt/homebrew/bin", cmd,
		"/usr/local/bin", cmd,
	} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}
