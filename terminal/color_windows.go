//go:build windows

package terminal

import "golang.org/x/sys/windows"

func isColorTerminal(descriptor uintptr) bool {
	const ansiMode = windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	handle := windows.Handle(descriptor)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&ansiMode == ansiMode {
		return true
	}
	return windows.SetConsoleMode(handle, mode|ansiMode) == nil
}
