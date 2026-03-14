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