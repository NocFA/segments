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
	return isProcessAlive(pid)
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
	notifyServerEvent("", nil)
}

// notifyServerEvent pings the running daemon so it can broadcast a WebSocket
// delta. If typ is non-empty, the payload is passed through as a WSMessage
// body; otherwise it is a plain sync ping (the server will still 204 but
// clients get no delta, which means they must wait for the next broadcast).
func notifyServerEvent(typ string, data interface{}) {
	pid, addr, err := pidFileData()
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pid); err != nil || p.Pid != pid {
		return
	}
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}
	var body []byte
	if typ != "" {
		body, _ = json.Marshal(map[string]interface{}{"type": typ, "data": data})
	}
	http.Post("http://"+addr+"/internal/sync", "application/json", bytes.NewReader(body))
}

// aliases maps user-facing command names to internal ones.
var aliases = map[string]string{
	"start":   "serve",
	"stop":    "stop",
	"list":    "list",
	"status":  "list",
	"install": "setup",
}

type cmdInfo struct {
	name    string
	desc    string
	aliases []string
}

var cmdGroups = []struct {
	title string
	cmds  []cmdInfo
}{
	{"Server", []cmdInfo{
		{"start", "start the server", []string{"serve"}},
		{"stop", "stop the server", nil},
	}},
	{"Tasks", []cmdInfo{
		{"list", "list projects and tasks", []string{"status"}},
		{"view", "view full task details", nil},
		{"add", "create a task", nil},
		{"done", "mark a task as done", nil},
		{"close", "close a task", nil},
		{"rename", "rename a project", nil},
	}},
	{"Setup", []cmdInfo{
		{"setup", "configure integrations (required first)", []string{"install"}},
		{"init", "initialize a project in the current directory", nil},
		{"beads", "import tasks from Beads", nil},
		{"remove", "remove a project", nil},
		{"uninstall", "remove segments and all data", nil},
	}},
	{"Info", []cmdInfo{
		{"help", "show this help", []string{"-h", "--help", "-help"}},
		{"version", "print version", nil},
	}},
}

func allCommandNames() []string {
	var names []string
	for _, g := range cmdGroups {
		for _, c := range g.cmds {
			names = append(names, c.name)
			names = append(names, c.aliases...)
		}
	}
	return names
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = curr[j-1] + 1
			if prev[j]+1 < curr[j] {
				curr[j] = prev[j] + 1
			}
			if prev[j-1]+cost < curr[j] {
				curr[j] = prev[j-1] + cost
			}
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func suggestCommand(input string) string {
	best := ""
	bestDist := 999
	for _, name := range allCommandNames() {
		d := levenshtein(input, name)
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	if bestDist <= 3 {
		return best
	}
	return ""
}

func runHelp() {
	fmt.Println()
	fmt.Println(bold.Render("Segments") + dim.Render(" -- task and project manager"))
	fmt.Println()
	for _, g := range cmdGroups {
		fmt.Println(bold.Render("  " + g.title))
		for _, c := range g.cmds {
			alias := ""
			if len(c.aliases) > 0 {
				alias = dim.Render("  (" + strings.Join(c.aliases, ", ") + ")")
			}
			fmt.Printf("    %s  %s%s\n", cyan.Render(padRight(c.name, 12)), dim.Render(c.desc), alias)
		}
		fmt.Println()
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func Run(args []string, version string) error {
	if len(args) < 2 {
		runHelp()
		return nil
	}

	cmd := args[1]
	rest := args[2:]

	if cmd == "help" || cmd == "-h" || cmd == "--help" || cmd == "-help" {
		runHelp()
		return nil
	}

	if mapped, ok := aliases[cmd]; ok {
		cmd = mapped
	}

	s := store.NewStore(expandPath(dataDir))

	// Gate most commands on setup completion. These pass through:
	//   setup, version, uninstall, mcp, context (invoked by integrations)
	switch cmd {
	case "setup", "version", "uninstall", "mcp", "context", "shell":
		// allowed
	default:
		if !setupComplete() {
			fmt.Fprintln(os.Stderr, red.Render("Segments is not set up."))
			fmt.Fprintln(os.Stderr, "  Run "+cyan.Render("sg setup")+" first to configure integrations.")
			os.Exit(1)
		}
	}

	switch cmd {
	case "serve":
		return runServe(s)
	case "stop":
		return runStop()
	case "init":
		return runInit(s)
	case "list":
		return runList(s, rest)
	case "view":
		return runView(s, rest)
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
		runHelp()
		return nil
	case "remove":
		return runRemoveProject(s, rest)
	case "uninstall":
		return runUninstall()
	case "version":
		fmt.Println(version)
		return nil
	case "mcp":
		return mcpServer(s)
	case "context":
		return runContext(s)
	default:
		if suggestion := suggestCommand(cmd); suggestion != "" {
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n  Did you mean %s?\n\n", cmd, cyan.Render(suggestion))
		} else {
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n  Run %s for available commands.\n\n", cmd, cyan.Render("sg help"))
		}
		os.Exit(1)
		return nil
	}
}

func ensureDaemon() (int, error) {
	if isRunning() {
		return getPID(), nil
	}
	ensureDataDir()
	cmd := exec.Command(os.Args[0], "serve")
	cmd.Env = append(os.Environ(), "SEGMENTS_DAEMON=1")
	logPath := filepath.Join(dataDir, "daemon.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	applyDaemonSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
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

	pid, err := ensureDaemon()
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	listenAddr := cfg.Bind + ":" + cfg.Port
	fmt.Println()
	fmt.Println(bold.Render("Segments started ") + green.Render("(pid: "+strconv.Itoa(pid)+")"))
	fmt.Println(dim.Render("  Listening: ") + cyan.Render("http://"+listenAddr))
	fmt.Println(bold.Render("Run: ") + cyan.Render("sg list") + dim.Render(" | sg help"))
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
	if fileExists(filepath.Join(cwd, ".beads", "issues.jsonl")) || fileExists(filepath.Join(cwd, "issues.jsonl")) {
		found = append(found, "  Issues: "+dim.Render("issues.jsonl found"))
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
	if err := stopProcess(pid); err != nil {
		return err
	}

	os.Remove(pidFile)

	fmt.Println()
	fmt.Println(bold.Render("Segments stopped ") + red.Render("(pid: "+strconv.Itoa(pid)+")"))
	fmt.Println()
	return nil
}

func resolveProject(projects []models.Project, hint string) *models.Project {
	if hint != "" {
		for i, p := range projects {
			if strings.HasPrefix(p.ID, hint) || strings.EqualFold(p.Name, hint) {
				return &projects[i]
			}
		}
		return nil
	}
	if len(projects) == 1 {
		return &projects[0]
	}
	cwd, _ := os.Getwd()
	dirName := filepath.Base(cwd)
	for i, p := range projects {
		if strings.EqualFold(p.Name, dirName) {
			return &projects[i]
		}
	}
	return nil
}

func statusStyle(status models.TaskStatus) lipgloss.Style {
	switch status {
	case models.StatusTodo:
		return dim
	case models.StatusInProgress:
		return cyan
	case models.StatusDone:
		return green
	case models.StatusClosed:
		return dim
	case models.StatusBlocker:
		return red
	default:
		return dim
	}
}

func printTasks(s *store.Store, proj *models.Project) {
	tasks, _ := s.ListTasks(proj.ID)
	if len(tasks) == 0 {
		fmt.Println("  No tasks.")
		return
	}

	var todo, inProgress, done, closed, blocker int
	for _, t := range tasks {
		switch t.Status {
		case models.StatusTodo:
			todo++
		case models.StatusInProgress:
			inProgress++
		case models.StatusDone:
			done++
		case models.StatusClosed:
			closed++
		case models.StatusBlocker:
			blocker++
		}
	}

	var counts []string
	if todo > 0 {
		counts = append(counts, dim.Render(strconv.Itoa(todo)+" todo"))
	}
	if inProgress > 0 {
		counts = append(counts, cyan.Render(strconv.Itoa(inProgress)+" active"))
	}
	if blocker > 0 {
		counts = append(counts, red.Render(strconv.Itoa(blocker)+" blocked"))
	}
	if done > 0 {
		counts = append(counts, green.Render(strconv.Itoa(done)+" done"))
	}
	if closed > 0 {
		counts = append(counts, dim.Render(strconv.Itoa(closed)+" closed"))
	}
	fmt.Println("  " + strings.Join(counts, dim.Render(" / ")))
	fmt.Println()

	for _, t := range tasks {
		if t.Status == models.StatusClosed || t.Status == models.StatusDone {
			continue
		}
		st := statusStyle(t.Status)
		tag := st.Render(string(t.Status))
		line := fmt.Sprintf("  %s  %-14s %s", dim.Render(t.ID[:8]), tag, t.Title)
		if t.Priority > 0 {
			line += "  " + priorityLabel(t.Priority)
		}
		if t.BlockedBy != "" {
			line += "  " + dim.Render("blocked:"+t.BlockedBy[:8])
		}
		fmt.Println(line)
	}
}

func priorityLabel(p int) string {
	switch p {
	case 3:
		return red.Render("P3")
	case 2:
		return yellow.Render("P2")
	case 1:
		return dim.Render("P1")
	default:
		return ""
	}
}

func findTaskByPrefix(s *store.Store, prefix string) (*models.Task, *models.Project, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return nil, nil, err
	}
	for _, p := range projects {
		tasks, _ := s.ListTasks(p.ID)
		for i := range tasks {
			if strings.HasPrefix(tasks[i].ID, prefix) {
				proj := p
				return &tasks[i], &proj, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("task not found: %s", prefix)
}

func runView(s *store.Store, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sg view <task-id>")
	}

	task, proj, err := findTaskByPrefix(s, args[0])
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(bold.Render(task.Title))
	fmt.Println()
	fmt.Printf("  %s  %s\n", dim.Render("ID:      "), task.ID)
	fmt.Printf("  %s  %s\n", dim.Render("Project: "), proj.Name+dim.Render(" ("+proj.ID[:8]+")"))

	st := statusStyle(task.Status)
	fmt.Printf("  %s  %s\n", dim.Render("Status:  "), st.Render(string(task.Status)))

	if task.Priority > 0 {
		fmt.Printf("  %s  %s\n", dim.Render("Priority:"), priorityLabel(task.Priority))
	}

	if task.BlockedBy != "" {
		blocker, _, _ := findTaskByPrefix(s, task.BlockedBy)
		blockerStr := task.BlockedBy
		if blocker != nil {
			blockerStr = blocker.Title + dim.Render(" ("+blocker.ID[:8]+")")
		}
		fmt.Printf("  %s  %s\n", dim.Render("Blocked: "), blockerStr)
	}

	fmt.Printf("  %s  %s\n", dim.Render("Created: "), task.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  %s  %s\n", dim.Render("Updated: "), task.UpdatedAt.Format("2006-01-02 15:04"))
	if task.ClosedAt != nil {
		fmt.Printf("  %s  %s\n", dim.Render("Closed:  "), task.ClosedAt.Format("2006-01-02 15:04"))
	}

	if task.Body != "" {
		fmt.Println()
		fmt.Println(dim.Render("  ---"))
		for _, line := range strings.Split(task.Body, "\n") {
			fmt.Println("  " + line)
		}
	}
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

	var hint string
	var showAll bool
	for _, a := range args {
		if a == "-a" || a == "--all" {
			showAll = true
		} else {
			hint = a
		}
	}

	if proj := resolveProject(projects, hint); proj != nil {
		fmt.Println()
		fmt.Println(bold.Render(proj.Name) + dim.Render("  "+proj.ID[:8]))
		printTasks(s, proj)
		if showAll {
			tasks, _ := s.ListTasks(proj.ID)
			hasClosed := false
			for _, t := range tasks {
				if t.Status == models.StatusDone || t.Status == models.StatusClosed {
					if !hasClosed {
						fmt.Println()
						fmt.Println(dim.Render("  Completed"))
					}
					hasClosed = true
					st := statusStyle(t.Status)
					tag := st.Render(string(t.Status))
					fmt.Printf("  %s  %-14s %s\n", dim.Render(t.ID[:8]), tag, dim.Render(t.Title))
				}
			}
		}
		fmt.Println()
		return nil
	}

	if hint != "" {
		if _, _, err := findTaskByPrefix(s, hint); err == nil {
			return runView(s, []string{hint})
		}
		return fmt.Errorf("no project or task matching: %s", hint)
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
		if done == len(tasks) && len(tasks) > 0 {
			color = green
		}
		fmt.Printf("  %s %s (%s%s)\n", cyan.Render(p.ID[:8]), bold.Render(p.Name), color.Render(progress), dim.Render(" done"))
	}
	fmt.Println()
	return nil
}

func runUninstall() error {
	for _, arg := range os.Args[1:] {
		if arg == "-f" || arg == "--force" {
			return doUninstall()
		}
	}

	if !confirm("Uninstall Segments?", "Removes all projects, tasks, server, and integrations.") {
		fmt.Println("Cancelled.")
		return nil
	}
	return doUninstall()
}

func runRemoveProject(s *store.Store, args []string) error {
	var hint string
	var force bool
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
		} else if hint == "" {
			hint = a
		}
	}

	if hint == "" {
		return fmt.Errorf("usage: sg remove <project-id|name> [--force]")
	}

	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	proj := resolveProject(projects, hint)
	if proj == nil {
		return fmt.Errorf("no project matching: %s", hint)
	}

	tasks, _ := s.ListTasks(proj.ID)

	if !force {
		fmt.Println()
		fmt.Println(red.Render("WARNING: this will permanently delete:"))
		fmt.Printf("  project %s (%s) and %d task(s)\n", bold.Render(proj.Name), proj.ID[:8], len(tasks))
		fmt.Println()
		fmt.Println("  Re-run with " + cyan.Render("--force") + " to confirm.")
		fmt.Println()
		return nil
	}

	if err := s.DeleteProject(proj.ID); err != nil {
		return err
	}
	fmt.Printf("Removed project %q (%s)\n", proj.Name, proj.ID[:8])
	notifyServer()
	return nil
}

func doUninstall() error {
	if isRunning() {
		stopProcess(getPID())
	}
	// Catch orphans the pid file didn't know about (stale pid, second install,
	// daemon started without updating the pid file).
	stopStrayDaemons()

	removeService()

	home, _ := os.UserHomeDir()
	os.RemoveAll(filepath.Join(home, ".segments"))

	for _, p := range []string{"segments", "sg", "segments.exe", "sg.exe"} {
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
	removeClaudeHook(filepath.Join(cwd, ".claude", "settings.json"))
	// Clean global integrations
	os.Remove(filepath.Join(home, ".pi", "agent", "extensions", "segments.ts"))
	if _, err := exec.LookPath("claude"); err == nil {
		exec.Command("claude", "mcp", "remove", "segments", "--scope", "user").Run()
	}
	// Drop legacy ~/.claude/mcp.json entry written by older setup runs.
	removeMCPEntry(filepath.Join(home, ".claude", "mcp.json"))
	removeClaudeHook(filepath.Join(home, ".claude", "settings.json"))

	cleanupSelf()

	fmt.Println("Segments removed.")
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

func setupMarkerPath() string {
	return filepath.Join(expandPath(dataDir), ".setup_complete")
}

func setupComplete() bool {
	return fileExists(setupMarkerPath())
}

func markSetupComplete() error {
	if err := os.MkdirAll(expandPath(dataDir), 0755); err != nil {
		return err
	}
	return os.WriteFile(setupMarkerPath(), []byte(""), 0644)
}

func runInit(s *store.Store) error {
	if err := ensureDataDir(); err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	dirName := filepath.Base(cwd)

	projects, _ := s.ListProjects()
	for _, p := range projects {
		if strings.EqualFold(p.Name, dirName) {
			fmt.Printf("Project %q already exists (%s)\n", p.Name, p.ID[:8])
			return nil
		}
	}

	// Offer beads import before creating the project; the import path
	// creates its own project to keep titles aligned with the directory.
	if isTerminal() {
		beadsPath := filepath.Join(cwd, ".beads", "issues.jsonl")
		if !fileExists(beadsPath) {
			beadsPath = filepath.Join(cwd, "issues.jsonl")
		}
		if fileExists(beadsPath) {
			if confirm("Import tasks from "+beadsPath+"?", "Creates a project "+dirName+" with tasks from "+beadsPath) {
				proj, err := s.CreateProject(dirName)
				if err != nil {
					return err
				}
				fmt.Printf("Created project %q (%s)\n", proj.Name, proj.ID[:8])
				if err := importBeads(s, proj.ID, beadsPath); err != nil {
					fmt.Println(red.Render(err.Error()))
				}
				offerMissingIntegrations(s, cwd)
				notifyServer()
				return nil
			}
		}
	}

	proj, err := s.CreateProject(dirName)
	if err != nil {
		return err
	}
	fmt.Printf("Created project %q (%s)\n", proj.Name, proj.ID[:8])

	offerMissingIntegrations(s, cwd)

	notifyServer()
	return nil
}

// offerMissingIntegrations prompts to set up local integrations for detected
// tools that are not already configured at either global or local scope.
func offerMissingIntegrations(s *store.Store, cwd string) {
	if !isTerminal() {
		return
	}
	home, _ := os.UserHomeDir()
	bin := filepath.Join(home, ".local", "bin", "segments")

	localIgs := buildIntegrations(s, scopeLocal, cwd, home, bin)
	globalIgs := buildIntegrations(s, scopeGlobal, cwd, home, bin)

	globalStatus := map[string]string{}
	for _, g := range globalIgs {
		globalStatus[g.name] = integrationStatus(g)
	}

	for _, ig := range localIgs {
		if !ig.detect() {
			continue
		}
		if globalStatus[ig.name] == "current" {
			continue
		}
		if integrationStatus(ig) == "current" {
			continue
		}
		fmt.Println("  " + yellow.Render("○") + " " + ig.name)
		if confirm(ig.prompt, ig.detail) {
			if err := ig.setup(); err != nil {
				fmt.Println("    " + red.Render(err.Error()))
			} else {
				fmt.Println("    " + green.Render("Done."))
			}
		}
	}
}

func importBeads(s *store.Store, projectID, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

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

		_, err = s.CreateTask(projectID, bead.Title, body, bead.Priority)
		if err != nil {
			skipped++
			continue
		}

		if bead.Status == "closed" {
			tasks, _ := s.ListTasks(projectID)
			if len(tasks) > 0 {
				last := tasks[len(tasks)-1]
				s.UpdateTask(projectID, last.ID, "", "", models.StatusClosed, -1, "")
			}
		}

		imported++
	}

	fmt.Println(bold.Render("Imported ") + green.Render(strconv.Itoa(imported)) + dim.Render(" tasks (") + yellow.Render(strconv.Itoa(skipped)) + dim.Render(" skipped)"))
	return nil
}

func runAdd(s *store.Store, args []string) error {
	var hint, title, body string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p":
			if i+1 < len(args) {
				hint = args[i+1]
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
	projectID, err := resolveProjectIDForMCP(s, hint)
	if err != nil {
		return err
	}

	t, err := s.CreateTask(projectID, title, body, 0)
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServerEvent("task:created", t)
	return nil
}

func runClose(s *store.Store, args []string) error {
	projectID, taskID, err := resolveProjectAndTaskArgs(s, args, "close")
	if err != nil {
		return err
	}
	t, err := s.UpdateTask(projectID, taskID, "", "", models.StatusClosed, -1, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServerEvent("task:updated", t)
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
	notifyServerEvent("project:updated", p)
	return nil
}

func runDone(s *store.Store, args []string) error {
	projectID, taskID, err := resolveProjectAndTaskArgs(s, args, "done")
	if err != nil {
		return err
	}
	t, err := s.UpdateTask(projectID, taskID, "", "", models.StatusDone, -1, "")
	if err != nil {
		return err
	}
	fmt.Println(t.ID)
	notifyServerEvent("task:updated", t)
	return nil
}

// resolveProjectAndTaskArgs supports both the 1-arg form (sg done <task_id>,
// project auto-resolved from the task lookup) and the legacy 2-arg form
// (sg done <project_hint> <task_id>). Task IDs may be UUID prefixes.
func resolveProjectAndTaskArgs(s *store.Store, args []string, cmd string) (string, string, error) {
	switch len(args) {
	case 1:
		task, proj, err := findTaskByPrefix(s, args[0])
		if err != nil {
			return "", "", err
		}
		return proj.ID, task.ID, nil
	case 2:
		projectID, err := resolveProjectIDForMCP(s, args[0])
		if err != nil {
			return "", "", err
		}
		tasks, _ := s.ListTasks(projectID)
		for i := range tasks {
			if strings.HasPrefix(tasks[i].ID, args[1]) {
				return projectID, tasks[i].ID, nil
			}
		}
		return "", "", fmt.Errorf("task not found in project: %s", args[1])
	default:
		return "", "", fmt.Errorf("usage: sg %s <task-id>  OR  sg %s <project-hint> <task-id>", cmd, cmd)
	}
}

func runBeads(s *store.Store, args []string) error {
	var beadsDir, beadsFile, projectName string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d":
			if i+1 < len(args) {
				beadsDir = args[i+1]
				i++
			}
		case "-f":
			if i+1 < len(args) {
				beadsFile = args[i+1]
				i++
			}
		case "-p":
			if i+1 < len(args) {
				projectName = args[i+1]
				i++
			}
		}
	}

	cwd, _ := os.Getwd()
	var beadsPath string
	switch {
	case beadsFile != "":
		beadsPath = beadsFile
	case beadsDir != "":
		beadsPath = filepath.Join(beadsDir, "issues.jsonl")
	default:
		beadsPath = filepath.Join(cwd, ".beads", "issues.jsonl")
		if !fileExists(beadsPath) {
			beadsPath = filepath.Join(cwd, "issues.jsonl")
		}
	}

	if !fileExists(beadsPath) {
		return fmt.Errorf("no issues.jsonl found (tried .beads/issues.jsonl and issues.jsonl in %s)", cwd)
	}

	if projectName == "" {
		projectName = filepath.Base(cwd)
	}

	proj, err := s.CreateProject(projectName)
	if err != nil {
		return err
	}
	fmt.Printf("Created project: %s %s\n", proj.ID, proj.Name)

	if err := importBeads(s, proj.ID, beadsPath); err != nil {
		return err
	}
	notifyServer()
	return nil
}

type installScope string

const (
	scopeGlobal installScope = "global"
	scopeLocal  installScope = "local"
)

type integration struct {
	name    string
	scope   installScope
	detect  func() bool
	path    func() string
	content func() string
	check   func() string // optional override for integrationStatus
	setup   func() error
	prompt  string
	detail  string
}

// integrationStatus returns the status of an integration:
// "missing" - not installed, "current" - installed and up to date, "outdated" - installed but content differs
func integrationStatus(ig integration) string {
	if ig.check != nil {
		return ig.check()
	}
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

	// Claude Code MCP + SessionStart hook
	if _, err := exec.LookPath("claude"); err == nil {
		var mcpPath, settingsPath string
		if scope == scopeGlobal {
			mcpPath = filepath.Join(home, ".claude.json")
			settingsPath = filepath.Join(home, ".claude", "settings.json")
		} else {
			mcpPath = filepath.Join(cwd, ".mcp.json")
			settingsPath = filepath.Join(cwd, ".claude", "settings.json")
		}
		igs = append(igs, integration{
			name:   "Claude Code",
			scope:  scope,
			detect: func() bool { return true },
			path:   func() string { return mcpPath },
			content: func() string { return "" },
			check: func() string {
				hasMCP := mcpConfigured(mcpPath)
				hasHook := claudeHookConfigured(settingsPath)
				if hasMCP && hasHook {
					return "current"
				}
				if hasMCP || hasHook {
					return "outdated"
				}
				return "missing"
			},
			setup: func() error {
				if err := writeClaudeCodeMCP(scope, mcpPath); err != nil {
					return err
				}
				return writeClaudeHook(settingsPath)
			},
			prompt: "Set up Claude Code integration?",
			detail: fmt.Sprintf("Registers MCP server and adds session hook (%s)", settingsPath),
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

	if scope == scopeGlobal {
		igs = append(igs, serviceIntegration(bin))
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

	if err := markSetupComplete(); err != nil {
		return err
	}

	cfg, _ := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	listenAddr := cfg.Bind + ":" + cfg.Port

	fmt.Println()
	fmt.Println(dim.Render("Server: ") + cyan.Render("http://"+listenAddr))
	fmt.Println(dim.Render("Tip: ") + cyan.Render("sg init") + dim.Render(" in a project directory to start tracking tasks"))
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

func claudeHookConfigured(settingsPath string) bool {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	hooks, _ := cfg["hooks"].(map[string]interface{})
	if hooks == nil {
		return false
	}
	sessionStart, _ := hooks["SessionStart"].([]interface{})
	for _, entry := range sessionStart {
		e, _ := entry.(map[string]interface{})
		innerHooks, _ := e["hooks"].([]interface{})
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "segments") && strings.Contains(cmd, "context") {
				return true
			}
		}
	}
	return false
}

func writeClaudeHook(settingsPath string) error {
	os.MkdirAll(filepath.Dir(settingsPath), 0755)

	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	hookCmd := "segments context"

	sessionStart, _ := hooks["SessionStart"].([]interface{})
	found := false
	for _, entry := range sessionStart {
		e, _ := entry.(map[string]interface{})
		innerHooks, _ := e["hooks"].([]interface{})
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "segments") {
				hm["command"] = hookCmd
				found = true
			}
		}
	}

	if !found {
		sessionStart = append(sessionStart, map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": hookCmd,
				},
			},
		})
	}

	hooks["SessionStart"] = sessionStart
	cfg["hooks"] = hooks

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

func removeClaudeHook(settingsPath string) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	hooks, _ := cfg["hooks"].(map[string]interface{})
	if hooks == nil {
		return
	}
	sessionStart, _ := hooks["SessionStart"].([]interface{})
	var kept []interface{}
	for _, entry := range sessionStart {
		e, _ := entry.(map[string]interface{})
		innerHooks, _ := e["hooks"].([]interface{})
		isSegments := false
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "segments") {
				isSegments = true
			}
		}
		if !isSegments {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = kept
	}
	if len(hooks) == 0 {
		delete(cfg, "hooks")
	}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(settingsPath, append(out, '\n'), 0644)
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

func writeClaudeCodeMCP(scope installScope, path string) error {
	if scope == scopeGlobal {
		// Use claude's own CLI so we don't hand-edit the large shared ~/.claude.json.
		// Remove first for idempotency; ignore failure if it wasn't registered.
		exec.Command("claude", "mcp", "remove", "segments", "--scope", "user").Run()
		out, err := exec.Command("claude", "mcp", "add", "--scope", "user", "segments", "segments", "mcp").CombinedOutput()
		if err != nil {
			return fmt.Errorf("claude mcp add: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cfg)
	}
	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers["segments"] = map[string]interface{}{
		"command": "segments",
		"args":    []string{"mcp"},
	}
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

const segmentsShortcutInstructions = `Segments is the persistent task tracker for this project. Tasks survive context wipes and outlive sessions; TodoWrite does not. Use Segments to plan multi-step work, scaffold upcoming tasks, track what is in progress, and capture follow-ups so they are not lost.

When to use it (proactively, without being asked):
  Planning           Break a feature or refactor into steps BEFORE coding. Use segments_create_tasks to stub the whole queue in one call.
  Scaffolding        Stub upcoming work as todo tasks so the queue is visible.
  Starting work      segments_update_task status=in_progress on the task you pick up. Keep at most one in_progress at a time.
  Finishing          segments_update_task status=done when the work lands.
  New scope          Capture every "we should also..." as a new todo task immediately so it survives a context wipe.
  "segment it" / "sg it" / "seg it" / "segment this" / "sg this" / "seg this"
                     Capture the current topic as a task right now, no clarifying questions.

Task body is the contract. Every body must be self-contained: what to do, relevant file paths, constraints, expected outcome. A fresh session with no history must be able to pick it up from the body alone.

Priority (integer 0-3; use numbers, NOT the words "high"/"medium"/"low"):
  1  URGENT. Do NOW. User flagged this as urgent, or it is actively blocking other work.
  2  NORMAL. Regular dev work the user wants done this session.
  3  BACKLOG. Future / idea / nice-to-have / "let's discuss later". Not this session.
  0  unset. Only for legacy tasks created before triage; never pick 0 when CREATING a task.
When you create a task, you MUST choose 1, 2, or 3 based on intent. Do not default to 0. If the user said "let's do X now" it is 2; if they said "sometime we should X" it is 3; if they said "drop everything and fix X" it is 1.

Blockers (blocked_by):
  Set blocked_by=<task_id> whenever task A literally cannot start until task B lands. This is NOT optional ornamentation -- it tells the agent what order to work in and what is currently "ready". Never create cycles.
  Common cases that ALWAYS need a blocker edge:
    - Greenfield scaffold: the bootstrap/init task (e.g. "Initialize project") blocks every downstream task. Link every other task in the batch to it.
    - Infra before feature: "Install Stripe SDK" blocks "Build Merch page".
    - Schema before use: "Add DB migration" blocks "Wire up form submission".
  In segments_create_tasks, use "#0".."#N" to reference earlier entries in the same batch; the server resolves these to real UUIDs. Prefer this over creating tasks in two separate calls just to get the UUIDs.

MCP tools (server name: "segments"). Your client may expose them under these exact names or with an "mcp__segments__" prefix (Claude Code does). Trust your client's own tool list; do not invent names. project_id is OPTIONAL on all task tools: it auto-resolves from CWD basename, single-project fallback, or $SEGMENTS_PROJECT_ID. If your client advertises no segments_* tools at all, the MCP server is not connected -- fall back to the CLI below; do not guess tool names.
  segments_list_projects()
  segments_list_tasks(project_id?, status?)
  segments_get_task(task_id, project_id?)
  segments_create_task(title, body?, priority?, blocked_by?, project_id?)
  segments_create_tasks(tasks: [{title, body?, priority?, blocked_by?}, ...], project_id?)
      Preferred for planning: scaffold the whole queue in one call. Use "#0".."#N" in blocked_by to reference earlier tasks in the same batch (resolved to their new UUIDs).
  segments_update_task(task_id, title?, body?, status?, priority?, blocked_by?, project_id?)
      status: todo | in_progress | done | closed | blocker
      Only provided fields change; omitted fields are preserved.
  segments_delete_task(task_id, project_id?)

CLI fallback (only if MCP tools are unavailable). -p is optional: sg auto-resolves project_id the same way MCP does (CWD basename / single-project / $SEGMENTS_PROJECT_ID). Pass -p only to override.
  sg list                                   List projects and tasks
  sg view <task_id>                         Show full task details
  sg add "<title>" -m "<body>"              Create a task (auto-resolves project)
  sg done <task_id>                         Mark task done (auto-resolves project)
  sg close <task_id>                        Close a task (auto-resolves project)

IDs below are full UUIDs, ready to paste into tool calls.`

func runContext(s *store.Store) error {
	projects, err := s.ListProjects()
	if err != nil || len(projects) == 0 {
		return nil
	}

	lines := []string{segmentsShortcutInstructions, ""}
	for _, p := range projects {
		tasks, _ := s.ListTasks(p.ID)
		var todo, inProgress, done, blocker int
		var open []string
		for _, t := range tasks {
			switch t.Status {
			case models.StatusTodo:
				todo++
				entry := fmt.Sprintf("  [todo] %s  task_id=%s", t.Title, t.ID)
				if t.Priority > 0 {
					entry += fmt.Sprintf(" P%d", t.Priority)
				}
				if t.BlockedBy != "" {
					entry += " blocked_by=" + t.BlockedBy
				}
				open = append(open, entry)
			case models.StatusInProgress:
				inProgress++
				entry := fmt.Sprintf("  [in_progress] %s  task_id=%s", t.Title, t.ID)
				if t.Priority > 0 {
					entry += fmt.Sprintf(" P%d", t.Priority)
				}
				open = append(open, entry)
			case models.StatusDone:
				done++
			case models.StatusBlocker:
				blocker++
				entry := fmt.Sprintf("  [blocker] %s  task_id=%s", t.Title, t.ID)
				open = append(open, entry)
			}
		}
		lines = append(lines, fmt.Sprintf("Project: %s  project_id=%s  (%d tasks: %d todo, %d in progress, %d done, %d blockers)",
			p.Name, p.ID, len(tasks), todo, inProgress, done, blocker))
		lines = append(lines, open...)
	}

	context := strings.Join(lines, "\n")
	escaped, _ := json.Marshal(context)

	fmt.Printf(`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":%s}}`, escaped)
	return nil
}

func mcpServer(s *store.Store) error {
	if _, err := ensureDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "segments: auto-start server failed: %v\n", err)
	}

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
		// JSON-RPC 2.0: messages without an id are notifications and MUST NOT
		// receive a response. Claude Code sends notifications/initialized after
		// initialize; replying to it (even with an error) is a protocol violation
		// that can prevent the client from registering our tools.
		if _, hasID := req["id"]; !hasID {
			continue
		}
		enc.Encode(handleMCP(s, req))
	}
}

// supportedMCPProtocolVersions lists every protocol version this server speaks,
// newest first. If the client's requested version is in this list we echo it
// back; otherwise we fall back to the first (latest) version we support.
var supportedMCPProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

func negotiateProtocolVersion(req map[string]interface{}) string {
	params, _ := req["params"].(map[string]interface{})
	requested, _ := params["protocolVersion"].(string)
	for _, v := range supportedMCPProtocolVersions {
		if v == requested {
			return v
		}
	}
	return supportedMCPProtocolVersions[0]
}

const mcpServerInstructions = `Segments tracks multi-session work as a dependency graph. Use segments_create_tasks to scaffold whole queues in ONE round-trip (the tasks argument MUST be a real JSON array, not a stringified one). Use segments_update_task status=in_progress when starting work and status=done when it lands. Capture every "we should also..." as a new task so it survives context wipes. project_id is optional: auto-resolves from CWD basename, single-project fallback, or $SEGMENTS_PROJECT_ID.

Priority (integer, required when creating): 1=urgent/NOW, 2=normal session work, 3=backlog/future/idea, 0=unset (legacy only; do NOT pick 0 when creating). Always pick 1/2/3 based on user intent.

blocked_by: set whenever task A literally cannot start until task B lands. In segments_create_tasks, use "#0".."#N" to reference earlier entries in the same batch. For greenfield scaffolds, the bootstrap/init task almost always blocks every downstream task -- link them.`

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
			"protocolVersion": negotiateProtocolVersion(req),
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{"listChanged": false},
			},
			"serverInfo":   map[string]string{"name": "segments", "version": "0.1.0"},
			"instructions": mcpServerInstructions,
		}
	case "tools/list":
		resp["result"] = map[string]interface{}{
			"tools": mcpToolDefs(),
		}
	case "tools/call":
		params, _ := req["params"].(map[string]interface{})
		tool, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		resp["result"] = map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": callTool(s, tool, args)}},
		}
	case "prompts/list":
		resp["result"] = map[string]interface{}{"prompts": []interface{}{}}
	case "resources/list":
		resp["result"] = map[string]interface{}{"resources": []interface{}{}}
	default:
		resp["error"] = map[string]interface{}{"code": -32601, "message": "method not found"}
	}

	return resp
}

func mcpToolDefs() []map[string]interface{} {
	prop := func(typ, desc string) map[string]interface{} {
		return map[string]interface{}{"type": typ, "description": desc}
	}
	schema := func(required []string, props map[string]interface{}) map[string]interface{} {
		s := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	optProject := prop("string", "Project ID. Optional: auto-resolves from CWD basename, single-project fallback, or $SEGMENTS_PROJECT_ID.")
	return []map[string]interface{}{
		{"name": "segments_list_projects", "description": "List all projects.",
			"inputSchema": schema(nil, map[string]interface{}{})},
		{"name": "segments_create_project", "description": "Create a project.",
			"inputSchema": schema([]string{"name"}, map[string]interface{}{
				"name": prop("string", "Project name"),
			})},
		{"name": "segments_rename_project", "description": "Rename a project.",
			"inputSchema": schema([]string{"project_id", "name"}, map[string]interface{}{
				"project_id": prop("string", "Project ID"),
				"name":       prop("string", "New name"),
			})},
		{"name": "segments_list_tasks", "description": "List tasks for a project. Returns all fields: id, title, status, priority, body, blocked_by, dates.",
			"inputSchema": schema(nil, map[string]interface{}{
				"project_id": optProject,
				"status":     prop("string", "Optional filter: todo | in_progress | done | closed | blocker"),
			})},
		{"name": "segments_create_task", "description": "Create a single task. For two or more tasks, ALWAYS prefer segments_create_tasks (one call, much cheaper, supports cross-task blocked_by refs).",
			"inputSchema": schema([]string{"title"}, map[string]interface{}{
				"project_id": optProject,
				"title":      prop("string", "Task title"),
				"body":       prop("string", "Self-contained description: what to do, file paths, constraints, expected outcome. A fresh session must be able to pick it up from this alone."),
				"priority":   prop("number", "Integer. 1=urgent/NOW, 2=normal session work, 3=backlog/future. Always pick 1, 2, or 3 when creating; 0 is legacy-only."),
				"blocked_by": prop("string", "Task ID of a hard blocker. Set this whenever task A literally cannot start until task B lands (e.g. bootstrap blocks all downstream work)."),
			})},
		{"name": "segments_create_tasks", "description": "Create multiple tasks in one call. PREFERRED for planning/scaffolding -- scaffold a whole queue in one round-trip instead of N separate calls. The 'tasks' argument MUST be a real JSON array of objects (NOT a JSON-encoded string). In blocked_by, '#0'..'#N' references earlier entries in the same batch (resolved to their new UUIDs) -- use this for dependency chains like bootstrap -> downstream tasks.",
			"inputSchema": schema([]string{"tasks"}, map[string]interface{}{
				"project_id": optProject,
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "JSON array of task objects (NOT a stringified array). Each object: {title, body?, priority?, blocked_by?}. priority is an integer 1/2/3 based on user intent.",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"title"},
						"properties": map[string]interface{}{
							"title":      prop("string", "Task title"),
							"body":       prop("string", "Self-contained description"),
							"priority":   prop("number", "Integer. 1=urgent/NOW, 2=normal session work, 3=backlog/future. Pick 1/2/3 based on user intent."),
							"blocked_by": prop("string", "Task ID or '#<index>' of an earlier entry in this batch. Use '#0' to link to the first task (e.g. when everything depends on a bootstrap task)."),
						},
					},
				},
			})},
		{"name": "segments_update_task", "description": "Update a task. Only provided fields are changed; omitted fields are preserved.",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
				"title":      prop("string", "New title"),
				"body":       prop("string", "New body/description"),
				"status":     prop("string", "todo | in_progress | done | closed | blocker"),
				"priority":   prop("number", "Integer. 1=urgent/NOW, 2=normal session work, 3=backlog/future. 0=unset (legacy only)."),
				"blocked_by": prop("string", "ID of blocking task"),
			})},
		{"name": "segments_delete_task", "description": "Delete a task.",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
			})},
		{"name": "segments_get_task", "description": "Get full task details including body, priority, blocked_by, and dates.",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
			})},
	}
}

// resolveProjectIDForMCP picks a project for MCP tool calls. Order: explicit
// hint (UUID prefix or name) -> $SEGMENTS_PROJECT_ID -> CWD basename match ->
// single-project fallback. Returns an error with available project names when
// resolution is ambiguous so the agent can correct itself.
func resolveProjectIDForMCP(s *store.Store, hint string) (string, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return "", err
	}
	if len(projects) == 0 {
		return "", fmt.Errorf("no projects exist. Run `sg init` or call segments_create_project first")
	}
	if hint != "" {
		if p := resolveProject(projects, hint); p != nil {
			return p.ID, nil
		}
		return "", fmt.Errorf("no project matches %q", hint)
	}
	if env := os.Getenv("SEGMENTS_PROJECT_ID"); env != "" {
		if p := resolveProject(projects, env); p != nil {
			return p.ID, nil
		}
	}
	if p := resolveProject(projects, ""); p != nil {
		return p.ID, nil
	}
	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = fmt.Sprintf("%s (%s)", p.Name, p.ID)
	}
	return "", fmt.Errorf("cannot auto-resolve project: %d exist [%s]. Pass project_id explicitly or set $SEGMENTS_PROJECT_ID", len(projects), strings.Join(names, ", "))
}

func callTool(s *store.Store, tool string, args map[string]interface{}) string {
	str := func(key string) string { v, _ := args[key].(string); return v }
	marshal := func(v interface{}) string { d, _ := json.Marshal(v); return string(d) }
	errMsg := func(err error) string { return marshal(map[string]string{"error": err.Error()}) }
	intArg := func(key string, def int) int {
		v, ok := args[key]
		if !ok {
			return def
		}
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return def
	}

	switch tool {
	case "segments_list_projects":
		list, _ := s.ListProjects()
		return marshal(list)
	case "segments_create_project":
		p, err := s.CreateProject(str("name"))
		if err != nil {
			return errMsg(err)
		}
		notifyServerEvent("project:created", p)
		return marshal(p)
	case "segments_rename_project":
		p, err := s.UpdateProject(str("project_id"), str("name"))
		if err != nil {
			return errMsg(err)
		}
		notifyServerEvent("project:updated", p)
		return marshal(p)
	case "segments_list_tasks":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		list, err := s.ListTasks(pid)
		if err != nil {
			return errMsg(err)
		}
		if filter := str("status"); filter != "" {
			var filtered []models.Task
			for _, t := range list {
				if string(t.Status) == filter {
					filtered = append(filtered, t)
				}
			}
			list = filtered
		}
		return marshal(list)
	case "segments_create_task":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		t, err := s.CreateTask(pid, str("title"), str("body"), intArg("priority", 0))
		if err != nil {
			return errMsg(err)
		}
		if blockedBy := str("blocked_by"); blockedBy != "" {
			t, err = s.UpdateTask(pid, t.ID, "", "", "", -1, blockedBy)
			if err != nil {
				return errMsg(err)
			}
		}
		notifyServerEvent("task:created", t)
		return marshal(t)
	case "segments_create_tasks":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		raw, ok := args["tasks"].([]interface{})
		if !ok {
			// Tolerate LLMs that stringify the array argument: parse it back into
			// a real array and continue. The schema description tells them not to
			// do this, but failing hard just burns tokens on a retry.
			if tasksStr, isStr := args["tasks"].(string); isStr {
				if perr := json.Unmarshal([]byte(tasksStr), &raw); perr != nil {
					return errMsg(fmt.Errorf("tasks must be a JSON array of objects, not a string. Received a string that failed to parse: %v", perr))
				}
			}
		}
		if len(raw) == 0 {
			return errMsg(fmt.Errorf("tasks must be a non-empty JSON array of {title, body?, priority?, blocked_by?} objects"))
		}
		created := make([]*models.Task, 0, len(raw))
		for i, item := range raw {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return errMsg(fmt.Errorf("tasks[%d] is not an object", i))
			}
			title, _ := obj["title"].(string)
			if title == "" {
				return errMsg(fmt.Errorf("tasks[%d].title is required", i))
			}
			body, _ := obj["body"].(string)
			priority := 0
			if p, ok := obj["priority"]; ok {
				if pf, ok := p.(float64); ok {
					priority = int(pf)
				}
			}
			blockedBy, _ := obj["blocked_by"].(string)
			if strings.HasPrefix(blockedBy, "#") {
				idx, perr := strconv.Atoi(blockedBy[1:])
				if perr != nil || idx < 0 || idx >= len(created) {
					return errMsg(fmt.Errorf("tasks[%d].blocked_by=%q: no earlier batch entry at that index", i, blockedBy))
				}
				blockedBy = created[idx].ID
			}
			t, err := s.CreateTask(pid, title, body, priority)
			if err != nil {
				return errMsg(fmt.Errorf("tasks[%d]: %v", i, err))
			}
			if blockedBy != "" {
				t, err = s.UpdateTask(pid, t.ID, "", "", "", -1, blockedBy)
				if err != nil {
					return errMsg(fmt.Errorf("tasks[%d] set blocked_by: %v", i, err))
				}
			}
			created = append(created, t)
		}
		notifyServerEvent("tasks:created", created)
		return marshal(created)
	case "segments_update_task":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		status := models.TaskStatus(str("status"))
		priority := intArg("priority", -1)
		t, err := s.UpdateTask(pid, str("task_id"), str("title"), str("body"), status, priority, str("blocked_by"))
		if err != nil {
			return errMsg(err)
		}
		notifyServerEvent("task:updated", t)
		return marshal(t)
	case "segments_delete_task":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		taskID := str("task_id")
		if err := s.DeleteTask(pid, taskID); err != nil {
			return errMsg(err)
		}
		notifyServerEvent("task:deleted", map[string]string{"id": taskID, "project_id": pid})
		return `{"deleted": true}`
	case "segments_get_task":
		pid, err := resolveProjectIDForMCP(s, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		t, err := s.GetTask(pid, str("task_id"))
		if err != nil {
			return errMsg(err)
		}
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
