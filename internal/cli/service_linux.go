//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func serviceIntegration(bin string) integration {
	home, _ := os.UserHomeDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "segments.service")

	return integration{
		name:    "Auto-start",
		scope:   scopeGlobal,
		detect:  func() bool { return true },
		path:    func() string { return unitPath },
		content: func() string { return systemdUnit(bin) },
		setup: func() error {
			os.MkdirAll(filepath.Dir(unitPath), 0755)
			if err := os.WriteFile(unitPath, []byte(systemdUnit(bin)), 0644); err != nil {
				return err
			}
			exec.Command("systemctl", "--user", "daemon-reload").Run()
			return exec.Command("systemctl", "--user", "enable", "segments.service").Run()
		},
		prompt: "Start Segments automatically on login?",
		detail: fmt.Sprintf("Creates systemd user service at %s", unitPath),
	}
}

func systemdUnit(bin string) string {
	return fmt.Sprintf(`[Unit]
Description=Segments task manager
After=default.target

[Service]
Type=simple
ExecStart=%s serve
Environment=SEGMENTS_DAEMON=1
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, bin)
}

func removeService() {
	exec.Command("systemctl", "--user", "disable", "segments.service").Run()
	exec.Command("systemctl", "--user", "stop", "segments.service").Run()
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, ".config", "systemd", "user", "segments.service"))
}
