//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

func isProcessAlive(pid int) bool {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	h, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}

func stopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

func cleanupSelf() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	tmp := self + ".uninstall"
	os.Rename(self, tmp)
	cmd := exec.Command("cmd", "/c", "ping -n 3 127.0.0.1 >nul & del \""+tmp+"\"")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Start()
}
