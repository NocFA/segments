//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const taskName = "SegmentsAutoStart"
const legacyTaskName = `Segments\AutoStart`

func autostartVBSPath() string {
	return filepath.Join(dataDir, "autostart.vbs")
}

func writeAutostartVBS(bin string) (string, error) {
	path := autostartVBSPath()
	escaped := strings.ReplaceAll(bin, `"`, `""`)
	content := `CreateObject("WScript.Shell").Run """` + escaped + `"" serve", 0, False` + "\r\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func serviceIntegration(bin string) integration {
	return integration{
		name:    "Auto-start",
		scope:   scopeGlobal,
		detect:  func() bool { return true },
		path:    func() string { return "" },
		content: func() string { return "" },
		check: func() string {
			out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/V", "/FO", "LIST").CombinedOutput()
			if err == nil {
				if strings.Contains(strings.ToLower(string(out)), "wscript.exe") {
					return "current"
				}
				return "outdated"
			}
			if err := exec.Command("schtasks", "/Query", "/TN", legacyTaskName).Run(); err == nil {
				return "outdated"
			}
			return "missing"
		},
		setup: func() error {
			exec.Command("schtasks", "/Delete", "/TN", legacyTaskName, "/F").Run()
			vbs, err := writeAutostartVBS(bin)
			if err != nil {
				return fmt.Errorf("write autostart.vbs: %w", err)
			}
			// wscript.exe is a GUI-subsystem host, so no console window
			// appears at logon. The VBS stub launches segments with
			// SW_HIDE, keeping the daemon fully invisible.
			// PowerShell Register-ScheduledTask creates a per-user task
			// without needing admin. schtasks /SC ONLOGON without /RU
			// implies "any user" which does require admin.
			script := fmt.Sprintf(
				`$u="$env:USERDOMAIN\$env:USERNAME";`+
					`$a=New-ScheduledTaskAction -Execute 'wscript.exe' -Argument '"%s"';`+
					`$t=New-ScheduledTaskTrigger -AtLogOn -User $u;`+
					`$p=New-ScheduledTaskPrincipal -UserId $u -LogonType Interactive;`+
					`$s=New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -Hidden;`+
					`Register-ScheduledTask -TaskName '%s' -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null`,
				strings.ReplaceAll(vbs, "'", "''"),
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
	os.Remove(autostartVBSPath())
}
