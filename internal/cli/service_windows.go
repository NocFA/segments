//go:build windows

package cli

import (
	"fmt"
	"os/exec"
)

const taskName = `Segments\AutoStart`

func serviceIntegration(bin string) integration {
	return integration{
		name:   "Auto-start",
		scope:  scopeGlobal,
		detect: func() bool { return true },
		path:   func() string { return "" },
		content: func() string { return "" },
		check: func() string {
			err := exec.Command("schtasks", "/Query", "/TN", taskName).Run()
			if err != nil {
				return "missing"
			}
			return "current"
		},
		setup: func() error {
			return exec.Command("schtasks", "/Create",
				"/TN", taskName,
				"/TR", fmt.Sprintf(`"%s" serve`, bin),
				"/SC", "ONLOGON",
				"/RL", "LIMITED",
				"/F",
			).Run()
		},
		prompt: "Start Segments automatically on login?",
		detail: "Creates a scheduled task to run at logon",
	}
}

func removeService() {
	exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
}
