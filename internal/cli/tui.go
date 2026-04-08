package cli

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	warnStyle = red.Bold(true)
	optStyle  = dim
	selStyle  = cyan.Bold(true)
)

type confirmM struct {
	title   string
	detail  string
	focus   int
	result  bool
}

func (m confirmM) Init() tea.Cmd { return nil }

func (m confirmM) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "left", "h", "up", "k":
			m.focus = 0
		case "right", "l", "down", "j":
			m.focus = 1
		case "enter":
			m.result = m.focus == 1
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmM) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(warnStyle.Render(" " + m.title + " "))
	b.WriteString("\n\n")
	if m.detail != "" {
		b.WriteString(" " + m.detail + "\n\n")
	}
	b.WriteString(dim.Render(" arrows to select, enter to confirm, q to cancel"))
	b.WriteString("\n\n")

	for i, label := range []string{"No", "Yes"} {
		if i == m.focus {
			b.WriteString(selStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(optStyle.Render(" " + label + " "))
		}
		b.WriteString("  ")
	}
	b.WriteString("\n")
	return box.Render(b.String())
}

func confirm(title, detail string) bool {
	m := confirmM{title: title, detail: detail}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return false
	}
	return final.(confirmM).result
}

// selectM is a TUI model for selecting from a list of options.
type selectM struct {
	title   string
	options []string
	details []string
	focus   int
	result  int // -1 means cancelled
}

func (m selectM) Init() tea.Cmd { return nil }

func (m selectM) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "k":
			if m.focus > 0 {
				m.focus--
			}
		case "down", "j":
			if m.focus < len(m.options)-1 {
				m.focus++
			}
		case "enter":
			m.result = m.focus
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.result = -1
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectM) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(selStyle.Render(" " + m.title + " "))
	b.WriteString("\n\n")
	b.WriteString(dim.Render(" arrows to select, enter to confirm, q to cancel"))
	b.WriteString("\n\n")
	for i, opt := range m.options {
		if i == m.focus {
			b.WriteString(selStyle.Render("  > " + opt))
		} else {
			b.WriteString(optStyle.Render("    " + opt))
		}
		if i < len(m.details) && m.details[i] != "" {
			b.WriteString(dim.Render("  " + m.details[i]))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return box.Render(b.String())
}

// selectOption shows a list of options and returns the selected index, or -1 if cancelled.
func selectOption(title string, options []string, details []string) int {
	m := selectM{title: title, options: options, details: details, result: -1}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return -1
	}
	return final.(selectM).result
}