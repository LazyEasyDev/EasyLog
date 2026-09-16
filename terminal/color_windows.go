//go:build windows

package terminal

import "golang.org/x/sys/windows"

func isColorTerminal(descriptor uintptr) bool {
	const ansiMode = windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(descriptor), &mode)
	return err == nil && mode&ansiMode == ansiMode
}
