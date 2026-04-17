//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func stopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(os.Interrupt)
}

// stopStrayDaemons kills any lingering segments processes owned by the current
// user, excluding this process. Used by uninstall to catch daemons that aren't
// in the pid file (stale pid, multiple installs, etc.).
func stopStrayDaemons() {
	out, err := exec.Command("pgrep", "-x", "segments").Output()
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, line := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(line)
		if err != nil || pid == self {
			continue
		}
		syscall.Kill(pid, syscall.SIGTERM)
	}
}

func cleanupSelf() {}
