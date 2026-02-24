package cli

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9eff")).Bold(true)
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true)
	optStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4a9eff")).Bold(true).Background(lipgloss.Color("#1a1a1a"))
	boxStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#333")).Padding(1, 2)
)

type setupM struct {
	cwd     string
	beads   bool
	opencode bool
	pi      bool
	focus   int
}

func (m setupM) Init() tea.Cmd { return nil }

func (m setupM) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.focus > 0 { m.focus-- }
		case "down", "j":
			if m.focus < 3 { m.focus++ }
		case "enter":
			return m, tea.Quit
		case "q", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m setupM) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(headerStyle.Render(" Segments Setup "))
	b.WriteString("\n\n")
	b.WriteString(subtleStyle.Render(" up/down arrows, Enter to run, q to quit"))
	b.WriteString("\n\n")

	opts := []struct{ l string; c, f bool }{
		{"[x] Import Beads issues", m.beads, m.focus == 0},
		{"[ ] Enable MCP (OpenCode)", m.opencode, m.focus == 1},
		{"[ ] Load Pi extension", m.pi, m.focus == 2},
		{"[ ] Done", false, m.focus == 3},
	}
	for _, o := range opts {
		pre, st := "  ", optStyle
		if o.f { pre, st = "> ", selStyle }
		b.WriteString(st.Render(pre + o.l))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render(" http://localhost:8765"))
	b.WriteString("\n")
	return boxStyle.Render(b.String())
}

func runSetupTUI() {
	cwd, _ := os.Getwd()
	m := setupM{cwd: cwd}
	if _, err := os.Stat(filepath.Join(cwd, ".beads", "issues.jsonl")); err == nil { m.beads = true }
	if _, err := os.Stat(filepath.Join(cwd, "opencode.json")); err == nil { m.opencode = true }
	if _, err := os.Stat(filepath.Join(cwd, ".pi", "extensions")); err == nil { m.pi = true }
	tea.NewProgram(m).Start()
}

type uninstallM struct{ focus int }

func (m uninstallM) Init() tea.Cmd { return nil }

func (m uninstallM) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h", "up", "k": m.focus = 0
		case "right", "l", "down", "j": m.focus = 1
		case "enter": return m, tea.Quit
		case "q", "esc":
			m.focus = -1
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m uninstallM) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(warnStyle.Render(" WARNING: Removes EVERYTHING! "))
	b.WriteString("\n\n")
	b.WriteString(" Projects, tasks, server, sg alias\n")
	b.WriteString("\n\n")
	b.WriteString(subtleStyle.Render(" left/right select, Enter confirm, q cancel"))
	b.WriteString("\n\n")
	opts := []string{"No", "Yes"}
	for i, o := range opts {
		if i == m.focus {
			b.WriteString(selStyle.Render("[" + o + "]"))
		} else {
			b.WriteString(optStyle.Render(" " + o + " "))
		}
		b.WriteString("  ")
	}
	b.WriteString("\n")
	return boxStyle.Render(b.String())
}

func runRemoveTUI() bool {
	m := uninstallM{focus: 0}
	tea.NewProgram(m).Start()
	return m.focus == 1
}