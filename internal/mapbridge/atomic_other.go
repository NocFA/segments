//go:build !windows

package mapbridge

import "os"

func replaceFile(from, to string) error { return os.Rename(from, to) }
