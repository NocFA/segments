package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codeberg.org/nocfa/segments/internal/analytics"
	"codeberg.org/nocfa/segments/internal/export"
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
	boxBg = lipgloss.AdaptiveColor{Dark: "#1f1f1f", Light: "#f1f1f1"}
	box   = lipgloss.NewStyle().
		Background(boxBg).
		Padding(1, 2)
)

var dataDir = func() string {
	if d := os.Getenv("SEGMENTS_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".segments")
}()

var pidFile = filepath.Join(dataDir, "pid")

var buildVersion = "dev"

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

var (
	exporterOnce sync.Once
	exporter     *export.Writer
)

func exportWriter() *export.Writer {
	exporterOnce.Do(func() {
		cfg, err := server.LoadConfig(filepath.Join(expandPath(dataDir), "config.yaml"))
		if err != nil || cfg == nil {
			exporter = export.NewWriter(export.Config{})
			return
		}
		exporter = export.NewWriter(cfg.JSONLExport)
	})
	return exporter
}

// notifyServerEvent pings the running daemon so it can broadcast a WebSocket
// delta, appends the event to the configured JSONL export file (if any),
// and records an analytics event tagged source=cli. Use
// notifyServerEventFrom to override the source tag (e.g. "mcp" from the
// MCP tool dispatch).
func notifyServerEvent(typ string, data interface{}) {
	notifyServerEventFrom("cli", typ, data)
}

func notifyServerEventFrom(source, typ string, data interface{}) {
	notifyServerEventFromAgent(source, typ, data, nil)
}

// notifyServerEventFromAgent is notifyServerEventFrom with an explicit agent
// override. Pass a non-nil agent when the caller knows the MCP client identity
// for this request directly (e.g. the daemon's /internal/mcp handler forwarded
// it from the originating shim). When agent is nil and source=="mcp", the
// global mcpAgent set by handleMCP("initialize") is used as a fallback.
func notifyServerEventFromAgent(source, typ string, data interface{}, agent *analytics.Agent) {
	if typ != "" {
		recordAnalyticsEventWithAgent(source, typ, data, agent)
		if typ == "tasks:created" {
			if batch, ok := data.([]*models.Task); ok {
				exportWriter().EmitBatch("task:created", batch)
			}
		} else {
			exportWriter().Emit(typ, data)
		}
	}
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

var (
	analyticsOnce sync.Once
	mcpAgentMu    sync.Mutex
	mcpAgent      *analytics.Agent
)

// initAnalytics configures the process-wide analytics writer. Called lazily
// from any path that might record events. If the caller (e.g. a test) has
// already set a default writer, initAnalytics leaves it alone so test
// fixtures aren't clobbered by production defaults.
func initAnalytics() {
	analyticsOnce.Do(func() {
		if analytics.Default() != nil {
			return
		}
		enabled := true
		cfg, _ := server.LoadConfig(filepath.Join(expandPath(dataDir), "config.yaml"))
		if cfg != nil && cfg.Analytics != nil {
			enabled = *cfg.Analytics
		}
		if os.Getenv("SEGMENTS_ANALYTICS") == "0" {
			enabled = false
		}
		path := filepath.Join(expandPath(dataDir), "events.jsonl")
		analytics.SetDefault(analytics.NewWriter(path, enabled))
	})
}

// setMCPAgent stashes the client identity from MCP initialize.params.clientInfo
// so subsequent analytics events in the same process are tagged with it.
func setMCPAgent(name, version string) {
	mcpAgentMu.Lock()
	defer mcpAgentMu.Unlock()
	if name == "" {
		mcpAgent = nil
		return
	}
	mcpAgent = &analytics.Agent{Name: name, Version: version}
}

func getMCPAgent() *analytics.Agent {
	mcpAgentMu.Lock()
	defer mcpAgentMu.Unlock()
	if mcpAgent == nil {
		return nil
	}
	cp := *mcpAgent
	return &cp
}

// eventTypeForUpdate promotes generic "task:updated" to a more specific
// verb based on the target status, so a dashboard can tell "claimed" from
// "completed" from a plain edit without inspecting from/to.
func eventTypeForUpdate(toStatus string) string {
	switch toStatus {
	case string(models.StatusInProgress):
		return "task:claimed"
	case string(models.StatusDone):
		return "task:completed"
	case string(models.StatusClosed):
		return "task:closed"
	}
	return "task:updated"
}

// recordAnalyticsEvent extracts task/project IDs from data, enriches the
// event type for status transitions, and appends an analytics event tagged
// with source (cli|mcp|web) and the current MCP agent when applicable.
func recordAnalyticsEvent(source, typ string, data interface{}) {
	recordAnalyticsEventWithAgent(source, typ, data, nil)
}

func recordAnalyticsEventWithAgent(source, typ string, data interface{}, agent *analytics.Agent) {
	initAnalytics()

	var projectID, taskID, toStatus string
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
	case []*models.Task:
		for _, t := range v {
			if t != nil {
				recordAnalyticsEventWithAgent(source, "task:created", t, agent)
			}
		}
		return
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
	}

	recordType := typ
	if typ == "task:updated" && toStatus != "" {
		recordType = eventTypeForUpdate(toStatus)
	}

	if agent == nil && source == "mcp" {
		agent = getMCPAgent()
	}

	analytics.Record(analytics.Event{
		Type:      recordType,
		Source:    source,
		Agent:     agent,
		ProjectID: projectID,
		TaskID:    taskID,
		ToStatus:  toStatus,
	})
}

// aliases maps user-facing command names to internal ones.
var aliases = map[string]string{
	"start":   "serve",
	"stop":    "stop",
	"list":    "list",
	"status":  "list",
	"install": "setup",
	"delete":  "rm",
	"n":       "next",
	"st":      "stats",
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
		{"next", "pick the best next task to work on", []string{"n"}},
		{"recent", "list recently closed tasks", nil},
		{"stats", "dashboard: progress, agents, recent activity", []string{"st"}},
		{"view", "view full task details", nil},
		{"add", "create a task", nil},
		{"done", "mark a task as done", nil},
		{"close", "close a task", nil},
		{"rm", "delete a task", []string{"delete"}},
		{"rename", "rename a project", nil},
	}},
	{"Setup", []cmdInfo{
		{"setup", "configure integrations (required first)", []string{"install"}},
		{"init", "initialize a project in the current directory", nil},
		{"beads", "import tasks from Beads", nil},
		{"export", "dump task state as JSONL for git-workflow snapshots", nil},
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
	buildVersion = version
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
	case "next":
		return runNext(s, rest)
	case "recent":
		return runRecent(s, rest)
	case "stats":
		return runStats(s, rest)
	case "view":
		return runView(s, rest)
	case "add":
		return runAdd(s, rest)
	case "done":
		return runDone(s, rest)
	case "close":
		return runClose(s, rest)
	case "rm":
		return runRemoveTask(s, rest)
	case "rename":
		return runRename(s, rest)
	case "beads":
		return runBeads(s, rest)
	case "export":
		return runExport(s, rest)
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
		return mcpServer()
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
	pid := cmd.Process.Pid
	cmd.Process.Release()
	return pid, nil
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
	cfg.Version = buildVersion

	dir := server.ExpandPath(cfg.DataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	s = store.NewStore(dir)
	hub := server.NewHub()
	srv := server.NewServer(s, hub, cfg, pidFile)
	srv.SetMCPHandler(mcpDaemonHandler(s))

	if cfg.Extension != "" {
		fmt.Printf("Extension: %s\n", cfg.Extension)
	}
	if cfg.EnableMCP {
		fmt.Println("MCP: enabled")
	}

	return srv.Start()
}

func runStop() error {
	pid := getPID()
	if pid == 0 {
		return fmt.Errorf("not running")
	}

	if !isProcessAlive(pid) {
		os.Remove(pidFile)
		fmt.Println()
		fmt.Println(bold.Render("Segments stopped ") + dim.Render("(pid "+strconv.Itoa(pid)+" was stale, cleaned up)"))
		fmt.Println()
		return nil
	}

	if err := stopProcess(pid); err != nil {
		os.Remove(pidFile)
		return fmt.Errorf("stop pid %d: %w (pid file cleaned up)", pid, err)
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

// statusGlyph returns a styled single rune for the task's state.
// blockedOpen overrides the glyph to the blocked marker for todo tasks whose
// blocker is still open, so a todo never renders with its neutral glyph when
// it is in fact unactionable.
func statusGlyph(status models.TaskStatus, blockedOpen bool) string {
	if status == models.StatusTodo && blockedOpen {
		return red.Render("\u00d7")
	}
	switch status {
	case models.StatusInProgress:
		return cyan.Render("\u25b6")
	case models.StatusTodo:
		return dim.Render("o")
	case models.StatusDone:
		return green.Render("\u2713")
	case models.StatusClosed:
		return dim.Render("\u25ce")
	case models.StatusBlocker:
		return red.Render("\u00d7")
	}
	return " "
}

// priorityChip returns the priority badge as a fixed 2-visible-width chip.
// P0/unset renders as two blank spaces so the priority column stays aligned
// across rows.
func priorityChip(p int) string {
	switch p {
	case 1:
		return red.Render("P1")
	case 2:
		return yellow.Render("P2")
	case 3:
		return dim.Render("P3")
	}
	return "  "
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

// humanAge returns a compact age string for durations: "<1m", "3m", "2h",
// "5d", "3w". Callers typically render it as "{age} old".
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	default:
		return strconv.Itoa(int(d.Hours()/(24*7))) + "w"
	}
}

// blockedOpenFor reports whether t has a blocker that has not yet landed.
// A missing blocker (stale reference) is treated as open so the task stays
// in the BLOCKED section rather than silently promoting to READY.
func blockedOpenFor(t models.Task, byID map[string]*models.Task) bool {
	if t.BlockedBy == "" {
		return false
	}
	b, ok := byID[t.BlockedBy]
	if !ok {
		return true
	}
	return b.Status != models.StatusDone && b.Status != models.StatusClosed
}

type taskSections struct {
	inProgress []models.Task
	ready      []models.Task
	blocked    []models.Task
	done       []models.Task
	byID       map[string]*models.Task
}

// partitionTasks groups tasks into the list view's sections. With showAll
// true, done and closed tasks go into .done sorted newest-first and capped
// at 5 for the RECENTLY DONE section; otherwise they are omitted.
func partitionTasks(tasks []models.Task, showAll bool) taskSections {
	byID := make(map[string]*models.Task, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = &tasks[i]
	}
	var out taskSections
	out.byID = byID
	for i := range tasks {
		t := tasks[i]
		blockedOpen := blockedOpenFor(t, byID)
		switch t.Status {
		case models.StatusInProgress:
			out.inProgress = append(out.inProgress, t)
		case models.StatusTodo:
			if blockedOpen {
				out.blocked = append(out.blocked, t)
			} else {
				out.ready = append(out.ready, t)
			}
		case models.StatusBlocker:
			out.blocked = append(out.blocked, t)
		case models.StatusDone, models.StatusClosed:
			if showAll {
				out.done = append(out.done, t)
			}
		}
	}
	if showAll {
		sort.Slice(out.done, func(i, j int) bool {
			return out.done[i].UpdatedAt.After(out.done[j].UpdatedAt)
		})
		if len(out.done) > 5 {
			out.done = out.done[:5]
		}
	}
	return out
}

// printTasks renders the sectioned task view for a single project:
// header line, IN PROGRESS / READY / BLOCKED sections, RECENTLY DONE (with
// -a), and a footer hint suggesting the next command.
func printTasks(s *store.Store, proj *models.Project, showAll bool) {
	tasks, _ := s.ListTasks(proj.ID)
	fmt.Println()
	if len(tasks) == 0 {
		fmt.Println("  " + dim.Render("No tasks yet in ") + bold.Render(proj.Name) + dim.Render("."))
		fmt.Println()
		if isTerminal() {
			fmt.Println(dim.Render("\u21b3 next: sg add \"your first task\" -m \"body\""))
			fmt.Println()
		}
		return
	}

	secs := partitionTasks(tasks, showAll)

	var done, closed int
	var last time.Time
	for _, t := range tasks {
		switch t.Status {
		case models.StatusDone:
			done++
		case models.StatusClosed:
			closed++
		}
		if t.UpdatedAt.After(last) {
			last = t.UpdatedAt
		}
	}

	parts := []string{bold.Render(proj.Name), dim.Render(strconv.Itoa(len(tasks)) + " tasks")}
	if n := len(secs.inProgress); n > 0 {
		parts = append(parts, cyan.Render(strconv.Itoa(n)+" in progress"))
	}
	if n := len(secs.ready); n > 0 {
		parts = append(parts, green.Render(strconv.Itoa(n)+" ready"))
	}
	if n := len(secs.blocked); n > 0 {
		parts = append(parts, red.Render(strconv.Itoa(n)+" blocked"))
	}
	if done > 0 {
		parts = append(parts, dim.Render(strconv.Itoa(done)+" done"))
	}
	if closed > 0 {
		parts = append(parts, dim.Render(strconv.Itoa(closed)+" closed"))
	}
	header := strings.Join(parts, dim.Render(" \u00b7 "))
	if !last.IsZero() {
		header += "  " + dim.Render("| updated "+last.Format("15:04"))
	}
	fmt.Println(header)
	fmt.Println()

	printSection("IN PROGRESS", cyan, secs.inProgress, secs.byID, false)
	printSection("READY", green, secs.ready, secs.byID, false)
	printSection("BLOCKED", red, secs.blocked, secs.byID, false)
	if showAll && len(secs.done) > 0 {
		printSection("RECENTLY DONE", dim, secs.done, secs.byID, true)
	}

	if isTerminal() {
		var hints []string
		if len(secs.ready) > 0 {
			hints = append(hints, "sg start "+secs.ready[0].ID[:8])
		}
		if !showAll && (done+closed) > 0 {
			hints = append(hints, "sg list -a")
		}
		if len(hints) > 0 {
			fmt.Println(dim.Render("\u21b3 next: " + strings.Join(hints, " \u00b7 ")))
			fmt.Println()
		}
	}
}

func printSection(label string, accent lipgloss.Style, tasks []models.Task, byID map[string]*models.Task, strikeTitle bool) {
	if len(tasks) == 0 {
		return
	}
	fmt.Println(accent.Render("\u258e") + " " + bold.Render(label) + dim.Render("  ("+strconv.Itoa(len(tasks))+")"))
	for _, t := range tasks {
		blockedOpen := blockedOpenFor(t, byID)
		glyph := statusGlyph(t.Status, blockedOpen)
		chip := priorityChip(t.Priority)
		title := t.Title
		if strikeTitle {
			title = dim.Strikethrough(true).Render(title)
		}
		line := fmt.Sprintf("  %s  %s  %s  %s",
			dim.Render(t.ID[:8]),
			glyph,
			chip,
			title,
		)
		if blockedOpen && !strikeTitle {
			line += "  " + red.Render("\u2190\u2500\u2500 "+t.BlockedBy[:8])
		}
		fmt.Println(line)
	}
	fmt.Println()
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

	var blockerTask *models.Task
	blockedOpen := false
	if task.BlockedBy != "" {
		bt, _, _ := findTaskByPrefix(s, task.BlockedBy)
		if bt != nil {
			blockerTask = bt
			blockedOpen = bt.Status != models.StatusDone && bt.Status != models.StatusClosed
		} else {
			blockedOpen = true
		}
	}

	glyph := statusGlyph(task.Status, blockedOpen)
	chip := priorityChip(task.Priority)
	age := humanAge(time.Since(task.CreatedAt))

	fmt.Println()
	fmt.Println("  " + bold.Render(task.Title))
	fmt.Println()
	meta := fmt.Sprintf("  %s  %s  %s",
		glyph, chip, dim.Render(task.ID[:8]))
	meta += dim.Render("  \u00b7  ") + proj.Name
	meta += dim.Render("  \u00b7  " + age + " old")
	fmt.Println(meta)

	if blockedOpen {
		blockerStr := red.Render("\u2190\u2500\u2500 " + task.BlockedBy[:8])
		if blockerTask != nil {
			blockerStr += "  " + dim.Render(blockerTask.Title)
		}
		fmt.Println("  " + blockerStr)
	}

	times := []string{"created " + task.CreatedAt.Format("2006-01-02 15:04")}
	if !task.UpdatedAt.Equal(task.CreatedAt) {
		times = append(times, "updated "+task.UpdatedAt.Format("2006-01-02 15:04"))
	}
	if task.ClosedAt != nil {
		times = append(times, "closed "+task.ClosedAt.Format("2006-01-02 15:04"))
	}
	fmt.Println("  " + dim.Render(strings.Join(times, " \u00b7 ")))

	if task.Body != "" {
		fmt.Println()
		fmt.Println("  " + dim.Render("\u2500\u2500\u2500"))
		for _, line := range strings.Split(task.Body, "\n") {
			fmt.Println("  " + line)
		}
	}

	if isTerminal() {
		var hint string
		switch {
		case task.Status == models.StatusTodo && blockedOpen:
			hint = "sg view " + task.BlockedBy[:8] + " for the blocker"
		case task.Status == models.StatusTodo:
			hint = "sg start " + task.ID[:8] + " to claim"
		case task.Status == models.StatusInProgress:
			hint = "sg done " + task.ID[:8] + " when finished"
		case task.Status == models.StatusBlocker:
			hint = "sg done " + task.ID[:8] + " to unblock dependents"
		}
		if hint != "" {
			fmt.Println()
			fmt.Println(dim.Render("\u21b3 " + hint))
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
	var noRecent bool
	for _, a := range args {
		switch a {
		case "-a", "--all":
			showAll = true
		case "--no-recent":
			noRecent = true
		default:
			hint = a
		}
	}

	if proj := resolveProject(projects, hint); proj != nil {
		printTasks(s, proj, showAll)
		if !noRecent {
			printRecentFooter(s, proj.ID)
		}
		return nil
	}

	if hint != "" {
		if _, _, err := findTaskByPrefix(s, hint); err == nil {
			return runView(s, []string{hint})
		}
		return fmt.Errorf("no project or task matching: %s", hint)
	}

	fmt.Println()
	fmt.Println(bold.Render("Projects") + dim.Render("  ("+strconv.Itoa(len(projects))+")"))
	fmt.Println()
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
		fmt.Printf("  %s  %s  %s\n", dim.Render(p.ID[:8]), bold.Render(p.Name), color.Render(progress)+dim.Render(" done"))
	}
	fmt.Println()
	if !noRecent {
		printRecentFooter(s, "")
	}
	return nil
}

// printRecentFooter appends a "Last 3 closed: ..." block to the list/status
// output. Looks back 30 days; nothing in window means no footer. projID
// scopes to one project; empty string scans all projects (with project tag).
func printRecentFooter(s *store.Store, projID string) {
	entries, err := collectRecentEntries(s, localMCPContext(), projID)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	entries = filterRecentEntries(entries, cutoff, 3)
	if len(entries) == 0 {
		return
	}
	fmt.Println(dim.Render("Last " + strconv.Itoa(len(entries)) + " closed:"))
	scoped := projID != ""
	for _, e := range entries {
		ts := e.Task.UpdatedAt
		if e.Task.ClosedAt != nil {
			ts = *e.Task.ClosedAt
		}
		line := fmt.Sprintf("  %s  %s  %s",
			dim.Render(e.Task.ID[:8]),
			e.Task.Title,
			dim.Render("("+relativeAgo(ts)+")"),
		)
		if !scoped {
			line += "  " + dim.Render(e.ProjectName)
		}
		fmt.Println(line)
	}
	fmt.Println()
}

// priorityBucket orders priorities 1 < 2 < 3 < unset so the "ready" queue
// surfaces the most important work first regardless of whether priority is
// set. Values outside 1-3 are treated as unset.
func priorityBucket(p int) int {
	if p < 1 || p > 3 {
		return 4
	}
	return p
}

// selectNextTask returns the ready-to-work candidates from a task list,
// sorted in the order sg next will present them: priority ascending (with
// unset last), then CreatedAt ascending. A task is a candidate when it is
// status=todo AND its blocker (if any) is done or closed.
func selectNextTask(tasks []models.Task) []models.Task {
	byID := make(map[string]*models.Task, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = &tasks[i]
	}
	var candidates []models.Task
	for i := range tasks {
		t := tasks[i]
		if t.Status != models.StatusTodo {
			continue
		}
		if blockedOpenFor(t, byID) {
			continue
		}
		candidates = append(candidates, t)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		pi, pj := priorityBucket(candidates[i].Priority), priorityBucket(candidates[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	return candidates
}

func runNext(s *store.Store, args []string) error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("No projects yet. Run " + cyan.Render("sg init") + " first.")
		return nil
	}

	var hint string
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			hint = args[i+1]
			i++
			continue
		}
		if hint == "" {
			hint = args[i]
		}
	}
	proj := resolveProject(projects, hint)
	if proj == nil {
		if hint != "" {
			return fmt.Errorf("no project matching: %s", hint)
		}
		return fmt.Errorf("could not resolve a project; pass -p <name>")
	}

	tasks, _ := s.ListTasks(proj.ID)
	ready := selectNextTask(tasks)

	if len(ready) == 0 {
		var inProg []models.Task
		for _, t := range tasks {
			if t.Status == models.StatusInProgress {
				inProg = append(inProg, t)
			}
		}
		fmt.Println()
		if len(inProg) > 0 {
			fmt.Println(dim.Render("No ready tasks, but you have ") + cyan.Render(strconv.Itoa(len(inProg))) + dim.Render(" in progress:"))
			fmt.Println()
			for _, t := range inProg {
				fmt.Printf("  %s  %s  %s  %s\n",
					dim.Render(t.ID[:8]),
					statusGlyph(t.Status, false),
					priorityChip(t.Priority),
					t.Title,
				)
			}
			fmt.Println()
			if isTerminal() {
				fmt.Println(dim.Render("\u21b3 sg done " + inProg[0].ID[:8] + " when finished"))
				fmt.Println()
			}
		} else {
			fmt.Println(dim.Render("No ready tasks yet \u2014 everything is done, blocked, or closed."))
			fmt.Println()
		}
		return nil
	}

	headline := ready[0]
	age := humanAge(time.Since(headline.CreatedAt))

	if !isTerminal() {
		fmt.Printf("NEXT: %s\n", headline.Title)
		fmt.Printf("%s P%d %s old no blockers\n", headline.ID[:8], headline.Priority, age)
		if len(ready) > 1 {
			fmt.Println("why: oldest unblocked at this priority")
			fmt.Println("also ready:")
			end := len(ready)
			if end > 4 {
				end = 4
			}
			for _, t := range ready[1:end] {
				fmt.Printf("  %s %s\n", t.ID[:8], t.Title)
			}
		} else {
			fmt.Println("why: only unblocked task")
		}
		fmt.Printf("sg start %s\n", headline.ID[:8])
		return nil
	}

	dimB := dim.Background(boxBg)
	boldB := bold.Background(boxBg)
	yellowB := yellow.Background(boxBg)
	redB := red.Background(boxBg)
	greenB := green.Background(boxBg)
	pChip := func(p int) string {
		switch p {
		case 1:
			return redB.Render("P1")
		case 2:
			return yellowB.Render("P2")
		case 3:
			return dimB.Render("P3")
		}
		return "  "
	}

	var inner strings.Builder
	inner.WriteString(dimB.Render("NEXT UP FOR YOU"))
	inner.WriteString("\n\n")
	inner.WriteString(boldB.Render(headline.Title))
	inner.WriteString("\n\n")
	meta := dimB.Render(headline.ID[:8])
	meta += dimB.Render("  \u00b7  ") + pChip(headline.Priority)
	meta += dimB.Render("  \u00b7  " + age + " old")
	meta += dimB.Render("  \u00b7  ") + greenB.Render("no blockers")
	inner.WriteString(meta)

	fmt.Println()
	fmt.Println(box.Render(inner.String()))
	fmt.Println()

	whyLine := dim.Render("why this? ready")
	if len(ready) > 1 {
		whyLine += dim.Render("  \u00b7  oldest ") + priorityChip(headline.Priority) + dim.Render(" that's unblocked")
	} else {
		whyLine += dim.Render("  \u00b7  only unblocked task")
	}
	fmt.Println(whyLine)

	if len(ready) > 1 {
		fmt.Println()
		fmt.Println(dim.Render("also ready:"))
		end := len(ready)
		if end > 4 {
			end = 4
		}
		for _, t := range ready[1:end] {
			fmt.Printf("  %s  %s  %s\n", green.Render("o"), dim.Render(t.ID[:8]), t.Title)
		}
	}

	fmt.Println()
	fmt.Println(dim.Render("\u21b3 sg start " + headline.ID[:8] + " to claim"))
	fmt.Println()
	return nil
}

// runRecent prints recently closed/done tasks across one or all projects.
// Mirrors the segments_recent MCP tool but renders human-readably with a
// "Xh ago" relative timestamp and optional project tag in cross-project mode.
// Flags: --limit N (default 10), --since DURATION, --project NAME.
func runRecent(s *store.Store, args []string) error {
	limit := 10
	sinceArg := ""
	projHint := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--limit" || a == "-n":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a number", a)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid %s value: %s", a, args[i+1])
			}
			limit = n
			i++
		case a == "--since":
			if i+1 >= len(args) {
				return fmt.Errorf("--since requires a duration")
			}
			sinceArg = args[i+1]
			i++
		case a == "--project" || a == "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a name", a)
			}
			projHint = args[i+1]
			i++
		default:
			return fmt.Errorf("unknown argument: %s", a)
		}
	}
	if limit <= 0 {
		limit = 10
	}

	var cutoff time.Time
	if sinceArg != "" {
		c, err := parseSince(time.Now(), sinceArg)
		if err != nil {
			return err
		}
		cutoff = c
	}

	entries, err := collectRecentEntries(s, localMCPContext(), projHint)
	if err != nil {
		return err
	}
	entries = filterRecentEntries(entries, cutoff, limit)

	fmt.Println()
	if len(entries) == 0 {
		fmt.Println("  " + dim.Render("No recently closed tasks."))
		fmt.Println()
		return nil
	}

	scoped := projHint != ""
	for _, e := range entries {
		ts := time.Time{}
		if e.Task.ClosedAt != nil {
			ts = *e.Task.ClosedAt
		} else {
			ts = e.Task.UpdatedAt
		}
		line := fmt.Sprintf("  %s  %s  %s",
			dim.Render(e.Task.ID[:8]),
			e.Task.Title,
			dim.Render("("+relativeAgo(ts)+")"),
		)
		if !scoped {
			line += "  " + dim.Render(e.ProjectName)
		}
		fmt.Println(line)
	}
	fmt.Println()
	return nil
}

func runStats(s *store.Store, args []string) error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("No projects yet. Run " + cyan.Render("sg init") + " first.")
		return nil
	}

	var hint string
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			hint = args[i+1]
			i++
			continue
		}
		if hint == "" {
			hint = args[i]
		}
	}
	proj := resolveProject(projects, hint)
	if proj == nil {
		if hint != "" {
			return fmt.Errorf("no project matching: %s", hint)
		}
		return fmt.Errorf("could not resolve a project; pass -p <name>")
	}

	initAnalytics()
	w := analytics.Default()
	if w == nil || !w.Enabled() {
		fmt.Println()
		fmt.Println(dim.Render("Analytics disabled in config (set ") + cyan.Render("analytics: true") + dim.Render(" in ~/.segments/config.yaml)."))
		fmt.Println()
		return nil
	}
	events, _ := analytics.Read(w.Path())
	if len(events) == 0 {
		fmt.Println()
		fmt.Println(dim.Render("No analytics data yet. Enabled by default \u2014 run some tasks to see stats."))
		fmt.Println()
		return nil
	}

	tasks, _ := s.ListTasks(proj.ID)
	secs := partitionTasks(tasks, false)
	var done, closed int
	for _, t := range tasks {
		switch t.Status {
		case models.StatusDone:
			done++
		case models.StatusClosed:
			closed++
		}
	}
	total := len(tasks)
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}

	var projEvents []analytics.Event
	for _, e := range events {
		if e.ProjectID == "" || e.ProjectID == proj.ID {
			projEvents = append(projEvents, e)
		}
	}

	col1 := buildStatsProjectColumn(proj.Name, done, len(secs.inProgress), len(secs.ready), len(secs.blocked), total, pct)
	col2 := buildStatsAgentsColumn(projEvents, tasks)
	col3 := buildStatsRecentColumn(projEvents)

	if !isTerminal() {
		fmt.Println()
		fmt.Println("-- " + strings.ToUpper(proj.Name) + " --")
		fmt.Println(col1)
		fmt.Println()
		fmt.Println("-- AGENTS --")
		fmt.Println(col2)
		fmt.Println()
		fmt.Println("-- RECENT --")
		fmt.Println(col3)
		fmt.Println()
		return nil
	}

	padCol := func(s string, w int) string {
		return lipgloss.NewStyle().Width(w).Padding(0, 1).Render(s)
	}
	fmt.Println()
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Top,
		padCol(col1, 34),
		padCol(col2, 40),
		padCol(col3, 46),
	))
	fmt.Println(dim.Render("\u21b3 sg next \u00b7 sg list \u00b7 sg graph"))
	fmt.Println()
	return nil
}

func buildStatsProjectColumn(name string, done, inProg, ready, blocked, total, pct int) string {
	header := bold.Render(strings.ToUpper(name))
	barWidth := 20
	var doneW, ipW int
	if total > 0 {
		doneW = done * barWidth / total
		ipW = inProg * barWidth / total
	}
	if doneW+ipW > barWidth {
		ipW = barWidth - doneW
	}
	restW := barWidth - doneW - ipW
	bar := green.Render(strings.Repeat("\u2593", doneW)) +
		yellow.Render(strings.Repeat("\u2593", ipW)) +
		dim.Render(strings.Repeat("\u2591", restW))

	parts := []string{
		green.Render(strconv.Itoa(done) + " done"),
		yellow.Render(strconv.Itoa(inProg) + " in-flight"),
		dim.Render(strconv.Itoa(ready) + " ready"),
	}
	if blocked > 0 {
		parts = append(parts, red.Render(strconv.Itoa(blocked)+" blocked"))
	}
	parts = append(parts, bold.Render(strconv.Itoa(pct)+"%"))
	counts := strings.Join(parts, dim.Render(" \u00b7 "))

	return header + "\n\n" + bar + "\n\n" + counts
}

func buildStatsAgentsColumn(events []analytics.Event, tasks []models.Task) string {
	header := bold.Render("AGENTS")
	if len(events) == 0 {
		return header + "\n\n" + dim.Render("no agents seen yet")
	}
	now := time.Now()

	statusByID := map[string]models.TaskStatus{}
	for _, t := range tasks {
		statusByID[t.ID] = t.Status
	}

	type agentInfo struct {
		name         string
		lastSeen     time.Time
		source       string
		currentClaim string
	}
	agents := map[string]*agentInfo{}
	claims := map[string]string{}
	for _, e := range events {
		actor := "you"
		if e.Agent != nil && e.Agent.Name != "" {
			actor = e.Agent.Name
		}
		if info, ok := agents[actor]; ok {
			if e.Timestamp.After(info.lastSeen) {
				info.lastSeen = e.Timestamp
				info.source = e.Source
			}
		} else {
			agents[actor] = &agentInfo{name: actor, lastSeen: e.Timestamp, source: e.Source}
		}
		if e.Type == "task:claimed" && e.TaskID != "" {
			claims[e.TaskID] = actor
		}
		if e.TaskID != "" && (e.Type == "task:completed" || e.Type == "task:closed" || e.Type == "task:deleted") {
			delete(claims, e.TaskID)
		}
	}
	for tid, agent := range claims {
		if statusByID[tid] == models.StatusInProgress {
			if info, ok := agents[agent]; ok {
				info.currentClaim = tid
			}
		}
	}

	var sorted []*agentInfo
	for _, a := range agents {
		sorted = append(sorted, a)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].lastSeen.After(sorted[j].lastSeen)
	})
	if len(sorted) > 6 {
		sorted = sorted[:6]
	}

	var lines []string
	for _, a := range sorted {
		age := now.Sub(a.lastSeen)
		var dot string
		var nameRender string
		if a.name == "you" {
			nameRender = bold.Render("you")
		} else {
			nameRender = cyan.Render(a.name)
		}
		switch {
		case a.currentClaim != "":
			dot = cyan.Render("\u25cf")
		case age < time.Hour:
			dot = yellow.Render("\u25d0")
		default:
			dot = dim.Render("o")
		}
		var line string
		if a.currentClaim != "" {
			line = fmt.Sprintf("%s %s %s %s  %s",
				dot, nameRender,
				dim.Render("on"), dim.Render(a.currentClaim[:8]),
				dim.Render(humanAge(age)+" \u00b7 "+a.source))
		} else {
			line = fmt.Sprintf("%s %s %s  %s",
				dot, nameRender,
				dim.Render("idle"),
				dim.Render(humanAge(age)+" \u00b7 "+a.source))
		}
		lines = append(lines, line)
	}
	return header + "\n\n" + strings.Join(lines, "\n")
}

func buildStatsRecentColumn(events []analytics.Event) string {
	header := bold.Render("RECENT")
	if len(events) == 0 {
		return header + "\n\n" + dim.Render("no activity yet")
	}
	sorted := make([]analytics.Event, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})
	if len(sorted) > 6 {
		sorted = sorted[:6]
	}

	var lines []string
	for _, e := range sorted {
		actor := "you"
		if e.Agent != nil && e.Agent.Name != "" {
			actor = e.Agent.Name
		}
		var actorRender string
		if actor == "you" {
			actorRender = bold.Render("you")
		} else {
			actorRender = cyan.Render(actor)
		}
		verb := eventVerb(e.Type)
		taskSnippet := ""
		if e.TaskID != "" {
			taskSnippet = dim.Render(e.TaskID[:8])
		}
		ts := e.Timestamp.Local().Format("15:04")
		line := fmt.Sprintf("%s %s %s %s %s",
			dim.Render(ts), dim.Render("\u00b7"),
			actorRender, dim.Render(verb), taskSnippet)
		lines = append(lines, line)
	}
	return header + "\n\n" + strings.Join(lines, "\n")
}

func eventVerb(typ string) string {
	switch typ {
	case "task:created":
		return "created"
	case "task:claimed":
		return "picked up"
	case "task:completed":
		return "completed"
	case "task:closed":
		return "closed"
	case "task:deleted":
		return "removed"
	case "task:updated":
		return "updated"
	case "project:created":
		return "made project"
	case "project:updated":
		return "renamed project"
	case "project:deleted":
		return "deleted project"
	}
	return typ
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
	notifyServerEvent("project:deleted", map[string]string{"id": proj.ID})
	if !isTerminal() {
		fmt.Printf("Removed project %q (%s)\n", proj.Name, proj.ID[:8])
		return nil
	}
	fmt.Printf("  %s  %s%s\n",
		dim.Render("-"),
		dim.Render(proj.Name),
		dim.Render("  ("+proj.ID[:8]+") removed"),
	)
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
	removeClaudeAllowlist(filepath.Join(cwd, ".claude", "settings.json"))
	// Clean global integrations
	os.Remove(filepath.Join(home, ".pi", "agent", "extensions", "segments.ts"))
	if _, err := exec.LookPath("claude"); err == nil {
		exec.Command("claude", "mcp", "remove", "segments", "--scope", "user").Run()
	}
	// Drop legacy ~/.claude/mcp.json entry written by older setup runs.
	removeMCPEntry(filepath.Join(home, ".claude", "mcp.json"))
	removeClaudeHook(filepath.Join(home, ".claude", "settings.json"))
	removeClaudeAllowlist(filepath.Join(home, ".claude", "settings.json"))

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
		yamlData := []byte(defaultConfigYAML)
		return os.WriteFile(cfgPath, yamlData, 0644)
	}
	return nil
}

const defaultConfigYAML = `port: "8765"
bind: "127.0.0.1"
data_dir: "~/.segments"

# jsonl_export:
#   enabled: true
#   path: ".segments/tasks.jsonl"
#   scope: all            # all | project
#   project_id: ""        # required when scope=project; UUID or prefix
#   on_events: []         # subset of [created, updated, done, closed, deleted]; empty = all
`

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
				notifyServerEvent("project:created", proj)
				if err := importBeads(s, proj.ID, beadsPath); err != nil {
					fmt.Println(red.Render(err.Error()))
				}
				offerMissingIntegrations(s, cwd)
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

	notifyServerEvent("project:created", proj)
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
	projectID, err := resolveProjectIDForMCP(s, localMCPContext(), hint)
	if err != nil {
		return err
	}

	t, err := s.CreateTask(projectID, title, body, 0)
	if err != nil {
		return err
	}
	notifyServerEvent("task:created", t)
	if !isTerminal() {
		fmt.Println(t.ID)
		return nil
	}
	glyph := statusGlyph(t.Status, false)
	chip := priorityChip(t.Priority)
	fmt.Println()
	fmt.Printf("  %s  %s  %s  %s\n", dim.Render(t.ID[:8]), glyph, chip, t.Title)
	fmt.Println()
	fmt.Println(dim.Render("\u21b3 sg start " + t.ID[:8] + " to claim"))
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
	notifyServerEvent("task:updated", t)
	if !isTerminal() {
		fmt.Println(t.ID)
		return nil
	}
	fmt.Printf("  %s  %s%s\n",
		dim.Render("\u25ce"),
		dim.Render(t.Title),
		dim.Render("  ("+t.ID[:8]+") closed"),
	)
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
	notifyServerEvent("project:updated", p)
	if !isTerminal() {
		fmt.Println(p.ID + " " + p.Name)
		return nil
	}
	fmt.Printf("  %s  %s %s\n",
		green.Render("\u2713"),
		dim.Render("renamed to"),
		bold.Render(p.Name),
	)
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
	notifyServerEvent("task:updated", t)
	if !isTerminal() {
		fmt.Println(t.ID)
		return nil
	}
	fmt.Printf("  %s  %s%s\n",
		green.Render("\u2713"),
		t.Title,
		dim.Render("  ("+t.ID[:8]+") marked done"),
	)
	return nil
}

func runRemoveTask(s *store.Store, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sg rm <task-id>")
	}
	task, proj, err := findTaskByPrefix(s, args[0])
	if err != nil {
		return err
	}
	if err := s.DeleteTask(proj.ID, task.ID); err != nil {
		return err
	}
	notifyServerEvent("task:deleted", map[string]string{"id": task.ID, "project_id": proj.ID})
	if !isTerminal() {
		fmt.Printf("Removed task %q (%s)\n", task.Title, task.ID[:8])
		return nil
	}
	fmt.Printf("  %s  %s%s\n",
		dim.Render("-"),
		dim.Render(task.Title),
		dim.Render("  ("+task.ID[:8]+") removed"),
	)
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
		projectID, err := resolveProjectIDForMCP(s, localMCPContext(), args[0])
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
	notifyServerEvent("project:created", proj)

	if err := importBeads(s, proj.ID, beadsPath); err != nil {
		return err
	}
	return nil
}

// runExport dumps a snapshot of tasks as JSONL. The default mode exports only
// the auto-resolved current project to .segments/tasks.jsonl so the file lives
// in the work tree as a git-friendly index. --all switches to a cross-project
// dump under ~/.segments/tasks.jsonl. --path overrides either default.
func runExport(s *store.Store, args []string) error {
	var path, hint string
	var all bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path", "-o":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case "--project", "-p":
			if i+1 < len(args) {
				hint = args[i+1]
				i++
			}
		case "--all", "-a":
			all = true
		case "-h", "--help":
			fmt.Println("usage: sg export [--path <file>] [--project <id|name>] [--all]")
			fmt.Println("  (default)     export the current project to .segments/tasks.jsonl (git-friendly)")
			fmt.Println("  --all, -a     export all projects to ~/.segments/tasks.jsonl")
			fmt.Println("  --path, -o    override output path (works with default and --all)")
			fmt.Println("  --project, -p restrict to a specific project by UUID prefix or name")
			fmt.Println()
			fmt.Println("Without --path, single-project mode honors jsonl_export.path from")
			fmt.Println("~/.segments/config.yaml as an override; --all always uses the home-dir default.")
			return nil
		default:
			if hint == "" && !strings.HasPrefix(args[i], "-") {
				hint = args[i]
			}
		}
	}

	if all && hint != "" {
		return fmt.Errorf("--all and --project are mutually exclusive")
	}

	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return fmt.Errorf("no projects to export")
	}

	var selected []models.Project
	if all {
		selected = projects
		if path == "" {
			path = filepath.Join(expandPath(dataDir), "tasks.jsonl")
		}
	} else {
		pid, err := resolveProjectIDForMCP(s, localMCPContext(), hint)
		if err != nil {
			return err
		}
		for i := range projects {
			if projects[i].ID == pid {
				selected = []models.Project{projects[i]}
				break
			}
		}
		if path == "" {
			cfg, _ := server.LoadConfig(filepath.Join(expandPath(dataDir), "config.yaml"))
			if cfg != nil && cfg.JSONLExport.Path != "" {
				path = cfg.JSONLExport.Path
			}
		}
		if path == "" {
			path = filepath.Join(".segments", "tasks.jsonl")
		}
	}

	var tasks []models.Task
	for _, p := range selected {
		ts, err := s.ListTasks(p.ID)
		if err != nil {
			return err
		}
		tasks = append(tasks, ts...)
	}

	w := export.NewWriter(export.Config{Enabled: true, Path: path})
	n, err := w.Snapshot(path, tasks, selected)
	if err != nil {
		return err
	}

	resolved, _ := export.ResolvePath(path)
	fmt.Printf("Wrote %d line(s) to %s\n", n, resolved)
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
				hasAllow := claudeAllowlistConfigured(settingsPath)
				if hasMCP && hasHook && hasAllow {
					return "current"
				}
				if hasMCP || hasHook || hasAllow {
					return "outdated"
				}
				return "missing"
			},
			setup: func() error {
				if err := writeClaudeCodeMCP(scope, mcpPath); err != nil {
					return err
				}
				if err := writeClaudeHook(settingsPath); err != nil {
					return err
				}
				return writeClaudeAllowlist(settingsPath)
			},
			prompt: "Set up Claude Code integration?",
			detail: fmt.Sprintf("Registers MCP server, adds session hook, and pre-approves segments tools (%s)", settingsPath),
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

const claudeAllowlistEntry = "mcp__segments"

func claudeAllowlistConfigured(settingsPath string) bool {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	perms, _ := cfg["permissions"].(map[string]interface{})
	if perms == nil {
		return false
	}
	allow, _ := perms["allow"].([]interface{})
	for _, entry := range allow {
		if s, _ := entry.(string); s == claudeAllowlistEntry {
			return true
		}
	}
	return false
}

func writeClaudeAllowlist(settingsPath string) error {
	os.MkdirAll(filepath.Dir(settingsPath), 0755)

	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &cfg)
	}

	perms, _ := cfg["permissions"].(map[string]interface{})
	if perms == nil {
		perms = map[string]interface{}{}
	}
	allow, _ := perms["allow"].([]interface{})

	for _, entry := range allow {
		if s, _ := entry.(string); s == claudeAllowlistEntry {
			return nil
		}
	}
	allow = append(allow, claudeAllowlistEntry)
	perms["allow"] = allow
	cfg["permissions"] = perms

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

func removeClaudeAllowlist(settingsPath string) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	perms, _ := cfg["permissions"].(map[string]interface{})
	if perms == nil {
		return
	}
	allow, _ := perms["allow"].([]interface{})
	var kept []interface{}
	for _, entry := range allow {
		if s, _ := entry.(string); s == claudeAllowlistEntry {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		delete(perms, "allow")
	} else {
		perms["allow"] = kept
	}
	if len(perms) == 0 {
		delete(cfg, "permissions")
	} else {
		cfg["permissions"] = perms
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

func runContext(s *store.Store) error {
	cfg, _ := server.LoadConfig(filepath.Join(dataDir, "config.yaml"))
	context := buildContextPayload(s, cfg)
	if context == "" {
		return nil
	}
	escaped, _ := json.Marshal(context)
	fmt.Printf(`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":%s}}`, escaped)
	return nil
}

// buildContextPayload assembles the additionalContext string emitted by
// `segments context`. The stable "how to use Segments" prose lives in
// mcpServerInstructions (surfaced by MCP clients without 2KB truncation);
// this hook emits only the CWD-resolved project's live state (in-progress
// + recently closed) so the payload stays small. Cross-project state is
// intentionally not dumped: the agent can call segments_list_projects or
// segments_recent on demand. Returns "" when the banner is disabled or
// there are no projects.
func buildContextPayload(s *store.Store, cfg *server.Config) string {
	if !sessionStartInjectEnabled(cfg) {
		return ""
	}
	projects, err := s.ListProjects()
	if err != nil || len(projects) == 0 {
		return ""
	}
	return segmentsContextBlock(s, projects)
}

// sessionStartInjectEnabled returns true when the segmentsContext block should
// be appended. Tri-state: nil (unset) and true both enable; only an explicit
// false disables.
func sessionStartInjectEnabled(cfg *server.Config) bool {
	if cfg == nil || cfg.SessionStartInject == nil {
		return true
	}
	return *cfg.SessionStartInject
}

// segmentsContextBlock renders the compact SessionStart banner: CWD-resolved
// project, up to 5 in-progress tasks, up to 5 recently closed tasks. When no
// project matches the CWD basename, emits a terse one-liner pointing at
// segments_list_projects (plus a git hint if CWD is a git repo); the agent
// can pull project data on demand instead of paying for a dump every session.
func segmentsContextBlock(s *store.Store, projects []models.Project) string {
	proj := resolveProject(projects, "")
	var b strings.Builder
	b.WriteString("# segmentsContext\n")
	if proj == nil {
		cwd, _ := os.Getwd()
		b.WriteString(fmt.Sprintf("No Segments project matches CWD basename %q. Call segments_list_projects to pick one, or `sg init` to create one.", filepath.Base(cwd)))
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			b.WriteString("\nGit repo detected: `git log --oneline -20` for recent history.")
		}
		return b.String()
	}
	b.WriteString(fmt.Sprintf("Project: %s  project_id=%s\n", proj.Name, proj.ID))

	tasks, _ := s.ListTasks(proj.ID)

	var inProgress []models.Task
	for _, t := range tasks {
		if t.Status == models.StatusInProgress {
			inProgress = append(inProgress, t)
		}
	}
	sort.Slice(inProgress, func(i, j int) bool {
		return inProgress[i].UpdatedAt.After(inProgress[j].UpdatedAt)
	})
	if len(inProgress) > 5 {
		inProgress = inProgress[:5]
	}
	b.WriteString("In-progress (up to 5):\n")
	if len(inProgress) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, t := range inProgress {
		b.WriteString(fmt.Sprintf("  %s  %s\n", t.ID[:8], t.Title))
	}

	entries := make([]recentEntry, 0, len(tasks))
	for _, t := range tasks {
		entries = append(entries, recentEntry{Task: t, ProjectID: proj.ID, ProjectName: proj.Name})
	}
	recent := filterRecentEntries(entries, time.Time{}, 5)
	b.WriteString("Recently closed (last 5):\n")
	if len(recent) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, e := range recent {
		ts := e.Task.UpdatedAt
		if e.Task.ClosedAt != nil {
			ts = *e.Task.ClosedAt
		}
		b.WriteString(fmt.Sprintf("  %s  %s (%s)\n", e.Task.ID[:8], e.Task.Title, relativeAgo(ts)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// mcpContext carries per-request state that the daemon-side MCP handler
// needs but that the shim alone knows: the shim's working directory (for
// CWD-basename project auto-resolution), the shim's SEGMENTS_PROJECT_ID
// override (same resolution chain), and the MCP client identity parsed
// from initialize.params.clientInfo so analytics attributes tool calls to
// the right agent even when several shims share one daemon.
type mcpContext struct {
	CWD          string
	ProjectIDEnv string
	Agent        *analytics.Agent
}

// localMCPContext returns the mcpContext for in-process dispatch (CLI paths
// and tests that run handleMCP directly). The daemon HTTP path builds its
// own mcpContext from the forwarded headers instead.
func localMCPContext() mcpContext {
	cwd, _ := os.Getwd()
	return mcpContext{CWD: cwd, ProjectIDEnv: os.Getenv("SEGMENTS_PROJECT_ID")}
}

// mcp request/response header names used by the shim<->daemon protocol.
// Kept together so the shim and the server agree on the wire.
const (
	mcpHeaderCWD          = "X-Segments-Cwd"
	mcpHeaderProjectID    = "X-Segments-Project-Id"
	mcpHeaderAgentName    = "X-Segments-Agent-Name"
	mcpHeaderAgentVersion = "X-Segments-Agent-Version"
)

// mcpDaemonHandler is wired into the server in runServeDaemon and invoked
// for each POST /internal/mcp request. It builds an mcpContext from the
// forwarded headers and dispatches to handleMCP.
func mcpDaemonHandler(s *store.Store) server.MCPHandler {
	return func(req map[string]interface{}, headers http.Header) map[string]interface{} {
		mc := mcpContext{
			CWD:          headers.Get(mcpHeaderCWD),
			ProjectIDEnv: headers.Get(mcpHeaderProjectID),
		}
		if name := headers.Get(mcpHeaderAgentName); name != "" {
			mc.Agent = &analytics.Agent{
				Name:    name,
				Version: headers.Get(mcpHeaderAgentVersion),
			}
		}
		return handleMCP(s, mc, req)
	}
}

// mcpServer is the stdio<->HTTP forwarder invoked by `segments mcp`. Each
// client (Claude Code, Pi, OpenCode) still spawns its own child process
// because MCP stdio transport is 1:1 with the client, but the child no
// longer opens LMDB: it decodes JSON-RPC from stdin, POSTs each request
// to the daemon's /internal/mcp endpoint (auto-starting the daemon if
// needed), and writes the response back to stdout byte-for-byte. The
// client's CWD, $SEGMENTS_PROJECT_ID, and clientInfo travel as headers so
// the daemon sees the same resolution inputs the old in-process handler
// would have seen.
func mcpServer() error {
	if _, err := ensureDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "segments: auto-start server failed: %v\n", err)
	}
	waitForDaemonReady(2 * time.Second)

	cwd, _ := os.Getwd()
	projectIDEnv := os.Getenv("SEGMENTS_PROJECT_ID")
	var agentName, agentVersion string

	client := &http.Client{Timeout: 30 * time.Second}

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

		// Capture the client's identity from the first initialize so every
		// subsequent request can forward it, keeping analytics attribution
		// stable even when the daemon is serving several shims at once.
		if method, _ := req["method"].(string); method == "initialize" {
			if params, ok := req["params"].(map[string]interface{}); ok {
				if ci, ok := params["clientInfo"].(map[string]interface{}); ok {
					agentName, _ = ci["name"].(string)
					agentVersion, _ = ci["version"].(string)
				}
			}
		}

		_, hasID := req["id"]
		if !hasID {
			// JSON-RPC 2.0 notifications MUST NOT receive a response.
			// Fire and forget; ignore errors so a daemon hiccup does not
			// cascade into a client-visible failure on a notification.
			go forwardMCP(client, req, cwd, projectIDEnv, agentName, agentVersion)
			continue
		}

		resp, err := forwardMCPWithRetry(client, req, cwd, projectIDEnv, agentName, agentVersion)
		if err != nil {
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error": map[string]interface{}{
					"code":    -32603,
					"message": fmt.Sprintf("segments daemon unavailable: %v", err),
				},
			}
		}
		enc.Encode(resp)
	}
}

// waitForDaemonReady polls until the daemon's pid file has a host:port
// entry and that port accepts a TCP connection, or deadline expires.
// ensureDaemon returns as soon as the child process is spawned, which is
// before it binds its listener; without this wait the very first
// forwardMCP can race the listener and trigger a superfluous retry.
func waitForDaemonReady(deadline time.Duration) {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, addr, err := pidFileData(); err == nil && addr != "" {
			if !strings.Contains(addr, ":") {
				addr = "127.0.0.1:" + addr
			}
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// forwardMCP POSTs a single JSON-RPC request to the daemon and returns the
// decoded response. A nil response with nil error means the daemon replied
// 204 No Content (notification path) and the caller should not write to
// stdout.
func forwardMCP(client *http.Client, req map[string]interface{}, cwd, projectIDEnv, agentName, agentVersion string) (map[string]interface{}, error) {
	pid, addr, err := pidFileData()
	if err != nil {
		return nil, err
	}
	if p, err := os.FindProcess(pid); err != nil || p.Pid != pid {
		return nil, fmt.Errorf("daemon pid %d not alive", pid)
	}
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", "http://"+addr+"/internal/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(mcpHeaderCWD, cwd)
	if projectIDEnv != "" {
		httpReq.Header.Set(mcpHeaderProjectID, projectIDEnv)
	}
	if agentName != "" {
		httpReq.Header.Set(mcpHeaderAgentName, agentName)
		httpReq.Header.Set(mcpHeaderAgentVersion, agentVersion)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned %s", resp.Status)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// forwardMCPWithRetry wraps forwardMCP with one daemon-restart retry so a
// transient crash or `sg stop` between requests doesn't wedge the client's
// session. If the retry still fails the caller turns the error into a
// JSON-RPC -32603 response.
func forwardMCPWithRetry(client *http.Client, req map[string]interface{}, cwd, projectIDEnv, agentName, agentVersion string) (map[string]interface{}, error) {
	resp, err := forwardMCP(client, req, cwd, projectIDEnv, agentName, agentVersion)
	if err == nil {
		return resp, nil
	}
	if _, restartErr := ensureDaemon(); restartErr != nil {
		return nil, fmt.Errorf("%v; restart failed: %v", err, restartErr)
	}
	for i := 0; i < 20; i++ {
		if isRunning() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return forwardMCP(client, req, cwd, projectIDEnv, agentName, agentVersion)
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

const mcpServerInstructions = `Segments is the persistent task tracker for this project. Tasks survive context wipes and outlive sessions; TodoWrite does not. Use Segments to plan multi-step work, scaffold upcoming tasks, track what is in progress, and capture follow-ups so they are not lost.

When to use it (proactively, without being asked):
  Planning           Break a feature or refactor into steps BEFORE coding. Use segments_create_tasks to stub the whole queue in ONE call with priority + blocked_by on every entry.
  Scaffolding        Stub upcoming work as todo tasks so the queue is visible.
  Starting / claiming work
                     Pick from the Ready queue (unblocked todos). IMMEDIATELY set status=in_progress to "claim" the task so other agents/sessions do not pick up the same work. If the user hands you multiple task IDs to work in sequence, claim ALL of them up front with segments_update_tasks (bulk) so every one is marked in_progress before you start task one; then process them one at a time. Claim only what you will actually work in this session; revert unwanted claims back to todo so others can pick them up.
  Finishing          segments_update_task status=done when the work lands (or segments_update_tasks to mark several done at once).
  New scope          Capture every "we should also..." as a new todo immediately so it survives a context wipe. If the follow-up was discovered while working on task X and cannot start until X lands, set blocked_by=<X's id> (the "discovered-from" pattern).
  "segment it" / "sg it" / "seg it" / "segment this" / "sg this" / "seg this"
                     Capture the current topic as a task right now, no clarifying questions.

Task body is the contract. Every body must be self-contained: what to do, relevant file paths, constraints, expected outcome. A fresh session with no history must be able to pick it up from the body alone.

Prefer the bulk variants (segments_create_tasks / segments_update_tasks / segments_delete_tasks) whenever you touch two or more tasks -- one round-trip, fewer tokens. The array argument for each MUST be a real JSON array, not a stringified one. project_id is optional on all task tools: auto-resolves from CWD basename, single-project fallback, or $SEGMENTS_PROJECT_ID.

Priority is an integer 1, 2, or 3 and is required on CREATE -- never omit it. Use numbers, NOT the words "high"/"medium"/"low". Match the user's signal:
  1  URGENT. "drop everything and fix X", "this is blocking prod", "broken build", "critical bug". Also: any task actively blocking other ready work.
  2  NORMAL. "let's do X", "add Y", "refactor Z", "ship the feature" -- regular session work. Default to 2 when the intent is clearly "do this now or next" but not urgent.
  3  BACKLOG. "sometime we should", "maybe later", "one idea is", "let's discuss". Not this session.
  0 is "unset" and exists only for legacy tasks. Never pick 0 when creating; default to 2 if genuinely unsure.

blocked_by is a correctness signal, not a hint. Set blocked_by=<task_id> whenever task A literally cannot start until task B lands. Omitting it when there is a real hard dependency misleads the next agent about which task is actionable.
  You MUST set blocked_by in these cases:
    - Greenfield scaffold: the bootstrap/init task blocks every downstream task. In a segments_create_tasks batch, put init as #0 and give every other task blocked_by="#0".
    - Infra before feature: "Install X" blocks "Use X". "Add DB migration" blocks "Query schema".
    - Discovered-from: follow-up discovered mid-work on X and cannot start until X is done -> blocked_by=<X's id>.
  Leave blocked_by empty only for genuinely independent tasks. "Do this after that" for flow reasons is handled by priority + list order, not blocked_by. Never create cycles.
  In segments_create_tasks, "#0".."#N" references earlier entries in the same batch; the server resolves these to real UUIDs. Creating a scaffolded batch without linking the obvious dependency chain is a correctness mistake, not a style choice.

Ready queue = todos whose blocker is empty or done. Pick from there first. The SessionStart hook prints a compact segmentsContext banner listing the CWD-resolved project, in-progress tasks, and recently closed tasks; use that to orient before querying.

MCP tools (server name: "segments"). Your client may expose them under these exact names or with an "mcp__segments__" prefix (Claude Code does). Trust your client's own tool list; do not invent names. If your client advertises no segments_* tools at all, the MCP server is not connected -- fall back to the CLI below.
  segments_list_projects()
  segments_list_tasks(project_id?, status?, fields?, limit?, since?, order_by?)
  segments_get_task(task_id, project_id?)
  segments_recent(project_id?, limit?, since?)  "What did we just finish?" -- compact list of recently closed tasks. Omit project_id to scan all projects.
  segments_create_task(title, body?, priority=1|2|3, blocked_by?, project_id?)
  segments_create_tasks(tasks: [{title, body?, priority=1|2|3, blocked_by?}, ...], project_id?)  Preferred for planning.
  segments_update_task(task_id, title?, body?, status?, priority?, blocked_by?, project_id?)  status: todo | in_progress | done | closed | blocker. Only provided fields change.
  segments_update_tasks(updates: [{task_id, ...}, ...], project_id?)  PREFERRED for claiming a run of tasks or marking several done at session end.
  segments_delete_task(task_id, project_id?)
  segments_delete_tasks(task_ids: [id1, id2, ...], project_id?)  PREFERRED whenever removing two or more tasks.

CLI fallback (only if MCP tools are unavailable). -p is optional: sg auto-resolves project_id the same way MCP does.
  sg list                                   List projects and tasks
  sg view <task_id>                         Show full task details
  sg add "<title>" -m "<body>"              Create a task
  sg done <task_id>                         Mark task done
  sg close <task_id>                        Close a task
  sg recent                                 Recently closed tasks

Schema deferral: Claude Code and other clients that use ToolSearch defer MCP tool schemas by default. If your tool list shows "mcp__segments__*" tools as deferred (schemas not loaded), issue this as your FIRST tool call of the session to load them before you need them:
  ToolSearch select:mcp__segments__segments_create_tasks,mcp__segments__segments_update_tasks,mcp__segments__segments_list_tasks,mcp__segments__segments_list_projects,mcp__segments__segments_update_task,mcp__segments__segments_create_task,mcp__segments__segments_get_task,mcp__segments__segments_delete_task,mcp__segments__segments_delete_tasks,mcp__segments__segments_create_project,mcp__segments__segments_rename_project,mcp__segments__segments_recent
For a persistent fix, set ENABLE_TOOL_SEARCH=false in Claude Code's environment -- that disables schema deferral globally. A "mcp__segments" entry in permissions.allow (written by sg setup) pre-authorizes every Segments tool so no permission prompts fire, but it does NOT flip schema loading; only ENABLE_TOOL_SEARCH does.`

func handleMCP(s *store.Store, mc mcpContext, req map[string]interface{}) map[string]interface{} {
	method, _ := req["method"].(string)
	id := req["id"]

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	switch method {
	case "initialize":
		if params, ok := req["params"].(map[string]interface{}); ok {
			if ci, ok := params["clientInfo"].(map[string]interface{}); ok {
				name, _ := ci["name"].(string)
				version, _ := ci["version"].(string)
				setMCPAgent(name, version)
			}
		}
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
			"content": []map[string]string{{"type": "text", "text": callTool(s, mc, tool, args)}},
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
		{"name": "segments_list_tasks", "description": "List tasks for a project. Returns compact rows by default (id, title, status, priority, blocked_by, closed_at, updated_at) so the body field stays out of the response. Pass fields=full when you actually need bodies. Responses over ~50KB are truncated and returned as a wrapper object {tasks, truncated, returned, total, hint} so the agent can narrow with since/limit/status/fields=compact. Use since with a duration (7d, 24h, 30m) or RFC3339 date to pull only recent activity.",
			"inputSchema": schema(nil, map[string]interface{}{
				"project_id": optProject,
				"status":     prop("string", "Optional filter: todo | in_progress | done | closed | blocker"),
				"limit":      prop("number", "Max tasks returned. Default 50. Non-positive values fall back to the default."),
				"since":      prop("string", "Only return tasks updated since this point. Accepts RFC3339 date or duration like 7d, 24h, 30m. When status=done|closed, filters by closed_at instead of updated_at."),
				"fields":     prop("string", "compact (default) omits body; full includes it. Prefer compact when scanning many tasks."),
				"order_by":   prop("string", "updated_at_desc (default) | closed_at_desc (auto when status=done|closed and order_by unset) | sort_order_asc"),
			})},
		{"name": "segments_create_task", "description": "Create a single task. For two or more tasks, ALWAYS prefer segments_create_tasks (one call, much cheaper, supports cross-task blocked_by refs). Always pass priority (1/2/3) and set blocked_by when a hard dependency exists.",
			"inputSchema": schema([]string{"title"}, map[string]interface{}{
				"project_id": optProject,
				"title":      prop("string", "Task title"),
				"body":       prop("string", "Self-contained description: what to do, file paths, constraints, expected outcome. A fresh session must be able to pick it up from this alone."),
				"priority":   prop("number", "Integer 1, 2, or 3 -- pick one every time you create. 1=URGENT (\"drop everything\", broken build, blocking other work). 2=NORMAL (regular session work; default when the intent is now-or-next). 3=BACKLOG (\"sometime\"/idea/future). 0 is legacy-unset -- do NOT pick 0 when creating."),
				"blocked_by": prop("string", "Task ID of a hard blocker. REQUIRED whenever this task literally cannot start until the blocker lands. Common cases: bootstrap blocks downstream, \"Install X\" blocks \"Use X\", schema migration blocks feature that queries it, task discovered while working on X -> blocked_by=<X>. Leave empty only for genuinely independent tasks."),
			})},
		{"name": "segments_create_tasks", "description": "Create multiple tasks in one call. PREFERRED for planning/scaffolding -- scaffold a whole queue in one round-trip instead of N separate calls. The 'tasks' argument MUST be a real JSON array of objects (NOT a JSON-encoded string). Set priority (1/2/3) on every entry. In blocked_by, '#0'..'#N' references earlier entries in the same batch (resolved to their new UUIDs). Link obvious dependency chains: for a greenfield scaffold, put the bootstrap/init task at #0 and every downstream task gets blocked_by=\"#0\". Creating a scaffold batch without linking obvious dependencies is a correctness mistake, not a style choice.",
			"inputSchema": schema([]string{"tasks"}, map[string]interface{}{
				"project_id": optProject,
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "JSON array of task objects (NOT a stringified array). Each object: {title, body?, priority, blocked_by?}. Set priority (1/2/3) on every task and blocked_by on every task that has a hard dependency.",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"title"},
						"properties": map[string]interface{}{
							"title":      prop("string", "Task title"),
							"body":       prop("string", "Self-contained description: what to do, file paths, constraints, expected outcome."),
							"priority":   prop("number", "Integer 1, 2, or 3 -- pick one per task. 1=URGENT (drop-everything, broken build, blocking other work). 2=NORMAL (regular session work; default when unsure). 3=BACKLOG (someday/idea/future). Do NOT pick 0 when creating."),
							"blocked_by": prop("string", "Task ID or '#<index>' of an earlier entry in this batch. Use '#0' when everything depends on a bootstrap task. REQUIRED whenever this task literally cannot start until the blocker lands (bootstrap -> downstream, Install X -> Use X, schema -> feature, discovered-from parent -> child). Omit ONLY for genuinely independent tasks."),
						},
					},
				},
			})},
		{"name": "segments_update_task", "description": "Update a single task. For two or more updates, ALWAYS prefer segments_update_tasks (one call, fewer tokens, atomic claim semantics). Only provided fields are changed; omitted fields are preserved. Use status=in_progress to claim a task when you start work and status=done when it lands. task_id is resolved across projects (full UUID or unique prefix), so passing just the id works even when the task lives in a different project than your CWD.",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
				"title":      prop("string", "New title"),
				"body":       prop("string", "New body/description"),
				"status":     prop("string", "todo | in_progress | done | closed | blocker. Set in_progress when you claim/pick up a task; done when the work lands."),
				"priority":   prop("number", "Integer. 1=URGENT (drop everything / blocking work). 2=NORMAL (regular session work). 3=BACKLOG (someday/idea/future). 0=unset is legacy-only."),
				"blocked_by": prop("string", "Task ID of a hard blocker (empty to clear). Set whenever this task literally cannot start until the blocker lands."),
			})},
		{"name": "segments_update_tasks", "description": "Update multiple tasks in one call. PREFERRED whenever you are changing two or more tasks -- one round-trip instead of N separate calls. The 'updates' argument MUST be a real JSON array of objects (NOT a JSON-encoded string). Use this to CLAIM a sequence of tasks (set status=in_progress on each) up front when the user hands you multiple task IDs to work through -- all downstream agents see the claim atomically instead of racing. Also use it to mark several tasks done at session end. Per-entry fields follow segments_update_task semantics.",
			"inputSchema": schema([]string{"updates"}, map[string]interface{}{
				"project_id": optProject,
				"updates": map[string]interface{}{
					"type":        "array",
					"description": "JSON array of update objects (NOT a stringified array). Each object: {task_id, title?, body?, status?, priority?, blocked_by?}. Only provided fields change; omitted fields are preserved per-task.",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"task_id"},
						"properties": map[string]interface{}{
							"task_id":    prop("string", "Task ID to update"),
							"title":      prop("string", "New title"),
							"body":       prop("string", "New body/description"),
							"status":     prop("string", "todo | in_progress | done | closed | blocker. Set in_progress to claim; done when work lands."),
							"priority":   prop("number", "Integer 1/2/3. 1=URGENT, 2=NORMAL, 3=BACKLOG. 0=unset is legacy-only."),
							"blocked_by": prop("string", "Task ID of a hard blocker (empty to clear)."),
						},
					},
				},
			})},
		{"name": "segments_delete_task", "description": "Delete a single task. For two or more deletes, ALWAYS prefer segments_delete_tasks. task_id is resolved across projects (full UUID or unique prefix).",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
			})},
		{"name": "segments_delete_tasks", "description": "Delete multiple tasks in one call. PREFERRED whenever removing two or more tasks -- one round-trip instead of N separate calls. The 'task_ids' argument MUST be a real JSON array of strings (NOT a JSON-encoded string).",
			"inputSchema": schema([]string{"task_ids"}, map[string]interface{}{
				"project_id": optProject,
				"task_ids": map[string]interface{}{
					"type":        "array",
					"description": "JSON array of task ID strings (NOT a stringified array).",
					"items":       map[string]interface{}{"type": "string"},
				},
			})},
		{"name": "segments_get_task", "description": "Get full task details including body, priority, blocked_by, and dates. task_id accepts a full UUID or a unique prefix; the task is found across all projects even when its project_id differs from your CWD, so passing just the id is enough. When resolution crosses projects, the response includes resolved_from_project_id so you can pass it on subsequent calls to skip the scan.",
			"inputSchema": schema([]string{"task_id"}, map[string]interface{}{
				"project_id": optProject,
				"task_id":    prop("string", "Task ID"),
			})},
		{"name": "segments_recent", "description": "Recent work summary: compact list of recently closed/done tasks, ordered by closed_at desc. This is the right tool for 'what did we just finish?', end-of-session recaps, and cross-session handoff context. Omit project_id to scan ALL projects (each row carries project_id/project_name for disambiguation); pass it to scope to one project. Body is never returned -- each row has a short summary (first line of body). Prefer this over segments_list_tasks(status=done) when you just want the headline.",
			"inputSchema": schema(nil, map[string]interface{}{
				"project_id": optProject,
				"limit":      prop("number", "Max rows returned. Default 10. Non-positive values fall back to the default."),
				"since":      prop("string", "Only return tasks closed since this point. Accepts RFC3339 date or duration like 7d, 24h, 30m. Tasks without a closed_at fall back to updated_at."),
			})},
	}
}

// resolveProjectIDForMCP picks a project for MCP tool calls. Order: explicit
// hint (UUID prefix or name) -> $SEGMENTS_PROJECT_ID -> CWD basename match ->
// single-project fallback. Returns an error with available project names when
// resolution is ambiguous so the agent can correct itself. CWD and the env
// override are read from mc so the daemon-side handler honors the originating
// shim's environment rather than its own.
func resolveProjectIDForMCP(s *store.Store, mc mcpContext, hint string) (string, error) {
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
	if mc.ProjectIDEnv != "" {
		if p := resolveProject(projects, mc.ProjectIDEnv); p != nil {
			return p.ID, nil
		}
	}
	if len(projects) == 1 {
		return projects[0].ID, nil
	}
	if mc.CWD != "" {
		dirName := filepath.Base(mc.CWD)
		for i := range projects {
			if strings.EqualFold(projects[i].Name, dirName) {
				return projects[i].ID, nil
			}
		}
	}
	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = fmt.Sprintf("%s (%s)", p.Name, p.ID)
	}
	return "", fmt.Errorf("cannot auto-resolve project: %d exist [%s]. Pass project_id explicitly or set $SEGMENTS_PROJECT_ID", len(projects), strings.Join(names, ", "))
}

// taskRef is the result of resolving a task reference (full ID or UUID prefix)
// across all projects for MCP reads/updates/deletes. If the caller passed a
// project_id hint that turned out not to contain the task, ResolvedFromOverride
// is true so the response can advertise where the task actually lives.
type taskRef struct {
	Task                 *models.Task
	ProjectID            string
	ResolvedFromOverride bool
}

// ambiguousTaskErr is returned when a prefix matches more than one task across
// projects. Handlers detect it with errors.As and emit a structured response
// listing candidates instead of just failing with a plain message.
type ambiguousTaskErr struct {
	Prefix     string
	Candidates []store.TaskMatch
}

func (e *ambiguousTaskErr) Error() string {
	return fmt.Sprintf("ambiguous task id %q matches %d tasks across projects", e.Prefix, len(e.Candidates))
}

// resolveTaskRef finds a task by full UUID or UUID prefix. Used by MCP
// read/update/delete handlers so a task created while CWD=project-A can be
// operated on from CWD=project-B. Semantics:
//  1. hintPID is honored only if explicitly passed (resolved via the project
//     hint format: name or UUID prefix). CWD auto-resolution is NOT applied
//     for reads; that was the bug this helper exists to fix.
//  2. If hintPID resolves and the task is there as a full UUID, fast-path to
//     Store.GetTask.
//  3. Otherwise scan all projects. Zero matches -> wrapped not-found error.
//     One match -> return it, flagging ResolvedFromOverride when the caller
//     passed a hintPID that turned out to be wrong. Many matches -> ambig err.
func resolveTaskRef(s *store.Store, hintPID, idOrPrefix string) (*taskRef, error) {
	if idOrPrefix == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	resolvedHint := ""
	if hintPID != "" {
		projects, err := s.ListProjects()
		if err != nil {
			return nil, err
		}
		if p := resolveProject(projects, hintPID); p != nil {
			resolvedHint = p.ID
		}
	}
	if resolvedHint != "" && len(idOrPrefix) == 36 {
		if t, err := s.GetTask(resolvedHint, idOrPrefix); err == nil {
			return &taskRef{Task: t, ProjectID: resolvedHint}, nil
		} else if !errors.Is(err, store.ErrTaskNotFound) {
			return nil, err
		}
	}
	matches, err := s.FindTaskAny(idOrPrefix)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("task %q not found in any project", idOrPrefix)
	}
	if len(matches) > 1 {
		return nil, &ambiguousTaskErr{Prefix: idOrPrefix, Candidates: matches}
	}
	m := matches[0]
	return &taskRef{
		Task:                 m.Task,
		ProjectID:            m.ProjectID,
		ResolvedFromOverride: resolvedHint != "" && resolvedHint != m.ProjectID,
	}, nil
}

// emitAmbigTaskErr formats an ambiguousTaskErr as the structured JSON response
// that MCP handlers return to the caller. Returns ("", false) when err is not
// an ambiguity error so the caller can fall back to plain error marshalling.
func emitAmbigTaskErr(s *store.Store, err error) (string, bool) {
	var ae *ambiguousTaskErr
	if !errors.As(err, &ae) {
		return "", false
	}
	nameOf := map[string]string{}
	if projects, perr := s.ListProjects(); perr == nil {
		for _, p := range projects {
			nameOf[p.ID] = p.Name
		}
	}
	cands := make([]map[string]string, 0, len(ae.Candidates))
	for _, c := range ae.Candidates {
		cands = append(cands, map[string]string{
			"id":           c.Task.ID,
			"project_id":   c.ProjectID,
			"project_name": nameOf[c.ProjectID],
			"title":        c.Task.Title,
		})
	}
	data, _ := json.Marshal(map[string]interface{}{
		"error":      "ambiguous task id",
		"prefix":     ae.Prefix,
		"candidates": cands,
	})
	return string(data), true
}

// marshalTaskWithResolve emits the task JSON, adding a resolved_from_project_id
// field when the ref was resolved from a different project than the caller's
// hint (so the agent can pass that PID next time and skip the cross-project
// scan).
func marshalTaskWithResolve(ref *taskRef) string {
	if ref == nil || ref.Task == nil {
		return `{"error": "nil task ref"}`
	}
	if !ref.ResolvedFromOverride {
		d, _ := json.Marshal(ref.Task)
		return string(d)
	}
	d, _ := json.Marshal(&struct {
		*models.Task
		ResolvedFromProjectID string `json:"resolved_from_project_id"`
	}{Task: ref.Task, ResolvedFromProjectID: ref.ProjectID})
	return string(d)
}

// coerceInt parses an integer argument received over MCP. The schema advertises
// "number", but real clients (especially when the field sits at the top level
// of a tool call) sometimes serialise it as a string. Accept float64, int,
// json.Number, and decimal strings; fall back to def on anything else.
func coerceInt(v interface{}, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n
		}
	}
	return def
}

// listTasksMaxBytes caps segments_list_tasks responses so full bodies don't
// blow past Claude Code's tool-result token budget on large projects. When the
// marshalled payload exceeds this, the handler drops items from the tail until
// the wrapped object fits, and advertises truncated=true so the agent can
// narrow.
const listTasksMaxBytes = 50 * 1024

// compactTask is the reduced shape returned by segments_list_tasks when
// fields=compact (the default). Drops Body and CreatedAt; keeps everything a
// ready-queue scan needs.
type compactTask struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Status    models.TaskStatus `json:"status"`
	Priority  int               `json:"priority"`
	BlockedBy string            `json:"blocked_by,omitempty"`
	ClosedAt  *time.Time        `json:"closed_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func toCompact(t models.Task) compactTask {
	return compactTask{
		ID:        t.ID,
		Title:     t.Title,
		Status:    t.Status,
		Priority:  t.Priority,
		BlockedBy: t.BlockedBy,
		ClosedAt:  t.ClosedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// parseSince turns a since= argument into an absolute cutoff time. Accepts
// RFC3339 timestamps and durations. "Nd" is recognised explicitly (Go's
// time.ParseDuration rejects days) in addition to standard h/m/s.
func parseSince(now time.Time, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if strings.HasSuffix(s, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && days >= 0 {
			return now.Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid since=%q: expected RFC3339 date or duration like 7d, 24h, 30m", s)
}

// renderListTasks applies the status/since filters, ordering, limit, fields
// projection, and 50KB size guard. Kept out of callTool so the logic is
// testable in isolation and the tool-dispatch switch stays compact.
func renderListTasks(list []models.Task, args map[string]interface{}) string {
	str := func(key string) string { v, _ := args[key].(string); return v }
	errMsg := func(err error) string {
		d, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(d)
	}

	statusFilter := str("status")
	isDoneOrClosed := statusFilter == "done" || statusFilter == "closed"

	if statusFilter != "" {
		filtered := make([]models.Task, 0, len(list))
		for _, t := range list {
			if string(t.Status) == statusFilter {
				filtered = append(filtered, t)
			}
		}
		list = filtered
	}

	if sinceStr := str("since"); sinceStr != "" {
		cutoff, perr := parseSince(time.Now(), sinceStr)
		if perr != nil {
			return errMsg(perr)
		}
		filtered := make([]models.Task, 0, len(list))
		for _, t := range list {
			ts := t.UpdatedAt
			if isDoneOrClosed && t.ClosedAt != nil {
				ts = *t.ClosedAt
			}
			if !ts.Before(cutoff) {
				filtered = append(filtered, t)
			}
		}
		list = filtered
	}

	orderBy := str("order_by")
	if orderBy == "" {
		if isDoneOrClosed {
			orderBy = "closed_at_desc"
		} else {
			orderBy = "updated_at_desc"
		}
	}
	switch orderBy {
	case "updated_at_desc":
		sort.Slice(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	case "closed_at_desc":
		sort.Slice(list, func(i, j int) bool {
			ai, aj := list[i].ClosedAt, list[j].ClosedAt
			switch {
			case ai == nil && aj == nil:
				return list[i].UpdatedAt.After(list[j].UpdatedAt)
			case ai == nil:
				return false
			case aj == nil:
				return true
			default:
				return ai.After(*aj)
			}
		})
	case "sort_order_asc":
		sort.Slice(list, func(i, j int) bool { return list[i].SortOrder < list[j].SortOrder })
	default:
		return errMsg(fmt.Errorf("invalid order_by=%q: expected updated_at_desc|closed_at_desc|sort_order_asc", orderBy))
	}

	total := len(list)

	limit := 50
	if v, ok := args["limit"]; ok {
		limit = coerceInt(v, 50)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit < len(list) {
		list = list[:limit]
	}

	fields := str("fields")
	if fields == "" {
		fields = "compact"
	}
	if fields != "compact" && fields != "full" {
		return errMsg(fmt.Errorf("invalid fields=%q: expected compact|full", fields))
	}

	build := func(n int, wrap bool) []byte {
		var items interface{}
		if fields == "compact" {
			c := make([]compactTask, n)
			for i := 0; i < n; i++ {
				c[i] = toCompact(list[i])
			}
			items = c
		} else {
			items = list[:n]
		}
		if wrap {
			d, _ := json.Marshal(map[string]interface{}{
				"tasks":     items,
				"truncated": true,
				"returned":  n,
				"total":     total,
				"hint":      "Narrow with since=, fields=compact, or limit=.",
			})
			return d
		}
		d, _ := json.Marshal(items)
		return d
	}

	payload := build(len(list), false)
	if len(payload) > listTasksMaxBytes {
		n := len(list)
		for n > 0 {
			n--
			payload = build(n, true)
			if len(payload) <= listTasksMaxBytes || n == 0 {
				break
			}
		}
	}
	return string(payload)
}

// recentTask is the row shape returned by segments_recent. Compact by design --
// body is never emitted, only a one-line summary derived from its first
// non-empty line.
type recentTask struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Status      models.TaskStatus `json:"status"`
	Priority    int               `json:"priority"`
	ClosedAt    *time.Time        `json:"closed_at,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	ProjectID   string            `json:"project_id,omitempty"`
	ProjectName string            `json:"project_name,omitempty"`
}

// filterRecentEntries keeps only done/closed entries, drops anything older than
// the cutoff (zero cutoff means no filter), sorts closed_at desc with
// updated_at as a fallback when ClosedAt is nil, then truncates to limit
// (non-positive limit means no cap). Shared by segments_recent (MCP) and
// sg recent (CLI) so both stay in lockstep.
func filterRecentEntries(entries []recentEntry, cutoff time.Time, limit int) []recentEntry {
	out := make([]recentEntry, 0, len(entries))
	for _, e := range entries {
		if e.Task.Status != models.StatusDone && e.Task.Status != models.StatusClosed {
			continue
		}
		if !cutoff.IsZero() {
			ts := e.Task.UpdatedAt
			if e.Task.ClosedAt != nil {
				ts = *e.Task.ClosedAt
			}
			if ts.Before(cutoff) {
				continue
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i].Task.ClosedAt, out[j].Task.ClosedAt
		switch {
		case ai == nil && aj == nil:
			return out[i].Task.UpdatedAt.After(out[j].Task.UpdatedAt)
		case ai == nil:
			return false
		case aj == nil:
			return true
		default:
			return ai.After(*aj)
		}
	})
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// relativeAgo renders a closed/updated timestamp as "<age> ago" using the
// existing humanAge formatter, or empty string for the zero time.
func relativeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return humanAge(time.Since(t)) + " ago"
}

// summaryFromBody returns the first non-empty line of body, trimmed and capped
// at 120 chars. Empty body -> empty string (summary field is omitted on wire).
func summaryFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 120 {
			line = line[:120]
		}
		return line
	}
	return ""
}

// renderRecentTasks builds the segments_recent response. The caller has already
// collected tasks from one or all projects and tagged each with its project
// name. Filters to status in {done, closed}, applies optional since cutoff,
// orders by closed_at desc (falling back to updated_at when ClosedAt is nil),
// truncates to limit (default 10).
type recentEntry struct {
	Task        models.Task
	ProjectID   string
	ProjectName string
}

func renderRecentTasks(entries []recentEntry, args map[string]interface{}) string {
	str := func(key string) string { v, _ := args[key].(string); return v }
	errMsg := func(err error) string {
		d, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(d)
	}

	var cutoff time.Time
	if sinceStr := str("since"); sinceStr != "" {
		c, perr := parseSince(time.Now(), sinceStr)
		if perr != nil {
			return errMsg(perr)
		}
		cutoff = c
	}

	limit := 10
	if v, ok := args["limit"]; ok {
		limit = coerceInt(v, 10)
	}
	if limit <= 0 {
		limit = 10
	}
	entries = filterRecentEntries(entries, cutoff, limit)

	rows := make([]recentTask, len(entries))
	for i, e := range entries {
		rows[i] = recentTask{
			ID:          e.Task.ID,
			Title:       e.Task.Title,
			Status:      e.Task.Status,
			Priority:    e.Task.Priority,
			ClosedAt:    e.Task.ClosedAt,
			Summary:     summaryFromBody(e.Task.Body),
			ProjectID:   e.ProjectID,
			ProjectName: e.ProjectName,
		}
	}
	d, _ := json.Marshal(rows)
	return string(d)
}

// collectRecentEntries pulls tasks from one or all projects, tagging each with
// its project name so renderRecentTasks can disambiguate. If hint is empty,
// iterates every project; otherwise resolves the hint to a single project.
func collectRecentEntries(s *store.Store, mc mcpContext, hint string) ([]recentEntry, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	if hint != "" {
		pid, err := resolveProjectIDForMCP(s, mc, hint)
		if err != nil {
			return nil, err
		}
		var chosen *models.Project
		for i := range projects {
			if projects[i].ID == pid {
				chosen = &projects[i]
				break
			}
		}
		if chosen == nil {
			return nil, fmt.Errorf("project %s vanished after resolution", pid)
		}
		tasks, err := s.ListTasks(pid)
		if err != nil {
			return nil, err
		}
		out := make([]recentEntry, len(tasks))
		for i, t := range tasks {
			out[i] = recentEntry{Task: t, ProjectID: chosen.ID, ProjectName: chosen.Name}
		}
		return out, nil
	}
	var out []recentEntry
	for _, p := range projects {
		tasks, err := s.ListTasks(p.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			out = append(out, recentEntry{Task: t, ProjectID: p.ID, ProjectName: p.Name})
		}
	}
	return out, nil
}

func callTool(s *store.Store, mc mcpContext, tool string, args map[string]interface{}) string {
	str := func(key string) string { v, _ := args[key].(string); return v }
	marshal := func(v interface{}) string { d, _ := json.Marshal(v); return string(d) }
	errMsg := func(err error) string { return marshal(map[string]string{"error": err.Error()}) }
	intArg := func(key string, def int) int {
		v, ok := args[key]
		if !ok {
			return def
		}
		return coerceInt(v, def)
	}
	notify := func(typ string, data interface{}) {
		notifyServerEventFromAgent("mcp", typ, data, mc.Agent)
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
		notify("project:created", p)
		return marshal(p)
	case "segments_rename_project":
		p, err := s.UpdateProject(str("project_id"), str("name"))
		if err != nil {
			return errMsg(err)
		}
		notify("project:updated", p)
		return marshal(p)
	case "segments_list_tasks":
		pid, err := resolveProjectIDForMCP(s, mc, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		list, err := s.ListTasks(pid)
		if err != nil {
			return errMsg(err)
		}
		return renderListTasks(list, args)
	case "segments_create_task":
		pid, err := resolveProjectIDForMCP(s, mc, str("project_id"))
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
		notify("task:created", t)
		return marshal(t)
	case "segments_create_tasks":
		pid, err := resolveProjectIDForMCP(s, mc, str("project_id"))
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
				priority = coerceInt(p, 0)
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
		notify("tasks:created", created)
		return marshal(created)
	case "segments_update_task":
		ref, err := resolveTaskRef(s, str("project_id"), str("task_id"))
		if err != nil {
			if out, ok := emitAmbigTaskErr(s, err); ok {
				return out
			}
			return errMsg(err)
		}
		status := models.TaskStatus(str("status"))
		priority := intArg("priority", -1)
		t, err := s.UpdateTask(ref.ProjectID, ref.Task.ID, str("title"), str("body"), status, priority, str("blocked_by"))
		if err != nil {
			return errMsg(err)
		}
		notify("task:updated", t)
		return marshalTaskWithResolve(&taskRef{Task: t, ProjectID: ref.ProjectID, ResolvedFromOverride: ref.ResolvedFromOverride})
	case "segments_update_tasks":
		raw, ok := args["updates"].([]interface{})
		if !ok {
			if updatesStr, isStr := args["updates"].(string); isStr {
				if perr := json.Unmarshal([]byte(updatesStr), &raw); perr != nil {
					return errMsg(fmt.Errorf("updates must be a JSON array of objects, not a string. Received a string that failed to parse: %v", perr))
				}
			}
		}
		if len(raw) == 0 {
			return errMsg(fmt.Errorf("updates must be a non-empty JSON array of {task_id, title?, body?, status?, priority?, blocked_by?} objects"))
		}
		hintPID := str("project_id")
		updated := make([]json.RawMessage, 0, len(raw))
		for i, item := range raw {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return errMsg(fmt.Errorf("updates[%d] is not an object", i))
			}
			taskID, _ := obj["task_id"].(string)
			if taskID == "" {
				return errMsg(fmt.Errorf("updates[%d].task_id is required", i))
			}
			ref, err := resolveTaskRef(s, hintPID, taskID)
			if err != nil {
				if out, ok := emitAmbigTaskErr(s, err); ok {
					return out
				}
				return errMsg(fmt.Errorf("updates[%d]: %v", i, err))
			}
			title, _ := obj["title"].(string)
			body, _ := obj["body"].(string)
			status := models.TaskStatus("")
			if s, ok := obj["status"].(string); ok {
				status = models.TaskStatus(s)
			}
			priority := -1
			if p, ok := obj["priority"]; ok {
				priority = coerceInt(p, -1)
			}
			blockedBy, _ := obj["blocked_by"].(string)
			t, err := s.UpdateTask(ref.ProjectID, ref.Task.ID, title, body, status, priority, blockedBy)
			if err != nil {
				return errMsg(fmt.Errorf("updates[%d]: %v", i, err))
			}
			notify("task:updated", t)
			updated = append(updated, json.RawMessage(marshalTaskWithResolve(&taskRef{Task: t, ProjectID: ref.ProjectID, ResolvedFromOverride: ref.ResolvedFromOverride})))
		}
		return marshal(updated)
	case "segments_delete_task":
		ref, err := resolveTaskRef(s, str("project_id"), str("task_id"))
		if err != nil {
			if out, ok := emitAmbigTaskErr(s, err); ok {
				return out
			}
			return errMsg(err)
		}
		if err := s.DeleteTask(ref.ProjectID, ref.Task.ID); err != nil {
			return errMsg(err)
		}
		notify("task:deleted", map[string]string{"id": ref.Task.ID, "project_id": ref.ProjectID})
		if ref.ResolvedFromOverride {
			return marshal(map[string]interface{}{"deleted": true, "resolved_from_project_id": ref.ProjectID})
		}
		return `{"deleted": true}`
	case "segments_delete_tasks":
		raw, ok := args["task_ids"].([]interface{})
		if !ok {
			if idsStr, isStr := args["task_ids"].(string); isStr {
				if perr := json.Unmarshal([]byte(idsStr), &raw); perr != nil {
					return errMsg(fmt.Errorf("task_ids must be a JSON array of strings, not a string. Received a string that failed to parse: %v", perr))
				}
			}
		}
		if len(raw) == 0 {
			return errMsg(fmt.Errorf("task_ids must be a non-empty JSON array of strings"))
		}
		hintPID := str("project_id")
		deleted := make([]map[string]string, 0, len(raw))
		for i, item := range raw {
			taskID, ok := item.(string)
			if !ok || taskID == "" {
				return errMsg(fmt.Errorf("task_ids[%d] must be a non-empty string", i))
			}
			ref, err := resolveTaskRef(s, hintPID, taskID)
			if err != nil {
				if out, ok := emitAmbigTaskErr(s, err); ok {
					return out
				}
				return errMsg(fmt.Errorf("task_ids[%d]: %v", i, err))
			}
			if err := s.DeleteTask(ref.ProjectID, ref.Task.ID); err != nil {
				return errMsg(fmt.Errorf("task_ids[%d]: %v", i, err))
			}
			notify("task:deleted", map[string]string{"id": ref.Task.ID, "project_id": ref.ProjectID})
			entry := map[string]string{"id": ref.Task.ID, "project_id": ref.ProjectID}
			if ref.ResolvedFromOverride {
				entry["resolved_from_project_id"] = ref.ProjectID
			}
			deleted = append(deleted, entry)
		}
		return marshal(map[string]interface{}{"deleted": deleted})
	case "segments_get_task":
		ref, err := resolveTaskRef(s, str("project_id"), str("task_id"))
		if err != nil {
			if out, ok := emitAmbigTaskErr(s, err); ok {
				return out
			}
			return errMsg(err)
		}
		return marshalTaskWithResolve(ref)
	case "segments_recent":
		entries, err := collectRecentEntries(s, mc, str("project_id"))
		if err != nil {
			return errMsg(err)
		}
		return renderRecentTasks(entries, args)
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
