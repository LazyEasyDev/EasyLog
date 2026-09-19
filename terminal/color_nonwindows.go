//go:build !windows

package terminal

import (
	"os"

	"golang.org/x/term"
)

func isColorTerminal(descriptor uintptr) bool {
	// Exact names only: color-capable variants must not be excluded by prefix.
	switch os.Getenv("TERM") {
	case "vt100", "vt102", "vt220":
		return false
	}
	return term.IsTerminal(int(descriptor))
}
