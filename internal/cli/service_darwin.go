//go:build darwin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

const launchAgentLabel = "net.nocfa.segments"

func serviceIntegration(bin string) integration {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")

	return integration{
		name:    "Auto-start",
		scope:   scopeGlobal,
		detect:  func() bool { return true },
		path:    func() string { return plistPath },
		content: func() string { return launchAgentPlist(bin) },
		setup: func() error {
			os.MkdirAll(filepath.Dir(plistPath), 0755)
			return os.WriteFile(plistPath, []byte(launchAgentPlist(bin)), 0644)
		},
		prompt: "Start Segments automatically on login?",
		detail: fmt.Sprintf("Creates LaunchAgent at %s", plistPath),
	}
}

func launchAgentPlist(bin string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>SEGMENTS_DAEMON</key>
        <string>1</string>
    </dict>
</dict>
</plist>
`, launchAgentLabel, bin)
}

func removeService() {
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"))
}
