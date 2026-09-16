//go:build !windows

package terminal

import "golang.org/x/term"

func isColorTerminal(descriptor uintptr) bool {
	return term.IsTerminal(int(descriptor))
}
