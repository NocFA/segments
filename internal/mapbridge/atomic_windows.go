//go:build windows

package mapbridge

import (
	"syscall"
	"unsafe"
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceFile(from, to string) error {
	fromPtr, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	const moveFileReplaceExisting = 0x1
	const moveFileWriteThrough = 0x8
	result, _, callErr := moveFileExW.Call(uintptr(unsafe.Pointer(fromPtr)), uintptr(unsafe.Pointer(toPtr)), moveFileReplaceExisting|moveFileWriteThrough)
	if result == 0 {
		return callErr
	}
	return nil
}
