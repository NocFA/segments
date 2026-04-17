//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	restoreVT()
}

func restoreVT() {
	enableVT(windows.Handle(os.Stdout.Fd()))
	enableVT(windows.Handle(os.Stderr.Fd()))
}

func enableVT(h windows.Handle) {
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
