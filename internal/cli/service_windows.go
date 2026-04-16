//go:build windows

package cli

import (
	"fmt"
	"os/exec"
	"strings"
)

const taskName = `Segments\AutoStart`

func serviceIntegration(bin string) integration {
	return integration{
		name:    "Auto-start",
		scope:   scopeGlobal,
		detect:  func() bool { return true },
		path:    func() string { return "" },
		content: func() string { return "" },
		check: func() string {
			if err := exec.Command("schtasks", "/Query", "/TN", taskName).Run(); err == nil {
				return "current"
			}
			// Fallback: list all tasks and scan for the name. Some Windows
			// versions misreport /TN lookups against backslash-pathed names.
			out, err := exec.Command("schtasks", "/Query", "/FO", "CSV", "/NH").CombinedOutput()
			if err == nil && strings.Contains(string(out), taskName) {
				return "current"
			}
			return "missing"
		},
		setup: func() error {
			out, err := exec.Command("schtasks", "/Create",
				"/TN", taskName,
				"/TR", fmt.Sprintf(`"%s" serve`, bin),
				"/SC", "ONLOGON",
				"/RL", "LIMITED",
				"/F",
			).CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return fmt.Errorf("schtasks: %w", err)
				}
				return fmt.Errorf("schtasks: %s", msg)
			}
			return nil
		},
		prompt: "Start Segments automatically on login?",
		detail: "Creates a scheduled task to run at logon",
	}
}

func removeService() {
	exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
}
