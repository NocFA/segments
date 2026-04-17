//go:build windows

package cli

import (
	"fmt"
	"os/exec"
	"strings"
)

const taskName = "SegmentsAutoStart"
const legacyTaskName = `Segments\AutoStart`

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
			if err := exec.Command("schtasks", "/Query", "/TN", legacyTaskName).Run(); err == nil {
				return "outdated"
			}
			return "missing"
		},
		setup: func() error {
			exec.Command("schtasks", "/Delete", "/TN", legacyTaskName, "/F").Run()
			// PowerShell Register-ScheduledTask creates a per-user task
			// without needing admin. schtasks /SC ONLOGON without /RU
			// implies "any user" which does require admin.
			script := fmt.Sprintf(
				`$u="$env:USERDOMAIN\$env:USERNAME";`+
					`$a=New-ScheduledTaskAction -Execute '%s' -Argument 'serve';`+
					`$t=New-ScheduledTaskTrigger -AtLogOn -User $u;`+
					`$p=New-ScheduledTaskPrincipal -UserId $u -LogonType Interactive;`+
					`$s=New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries;`+
					`Register-ScheduledTask -TaskName '%s' -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null`,
				strings.ReplaceAll(bin, "'", "''"),
				taskName,
			)
			out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return fmt.Errorf("Register-ScheduledTask: %w", err)
				}
				return fmt.Errorf("%s", msg)
			}
			return nil
		},
		prompt: "Start Segments automatically on login?",
		detail: "Creates a scheduled task to run at logon",
	}
}

func removeService() {
	exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
	exec.Command("schtasks", "/Delete", "/TN", legacyTaskName, "/F").Run()
}
