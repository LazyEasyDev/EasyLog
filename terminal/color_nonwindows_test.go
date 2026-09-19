//go:build !windows

package terminal

import (
	"testing"

	"github.com/creack/pty"
)

func TestUnixColorDetectionOnPTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	for _, terminalName := range []string{"dumb", "vt100", "vt102", "vt220"} {
		t.Run(terminalName, func(t *testing.T) {
			t.Setenv("TERM", terminalName)
			t.Setenv("NO_COLOR", "")
			if New(slave, nil).formatter.ForceColors {
				t.Fatal("monochrome terminal received automatic colors")
			}
			formatter := DefaultTextFormatter()
			formatter.ForceColors = true
			if !New(slave, &formatter).formatter.ForceColors {
				t.Fatal("explicit force did not override monochrome terminal")
			}
			formatter.DisableColors = true
			if New(slave, &formatter).formatter.ForceColors {
				t.Fatal("disable did not override force")
			}
		})
	}
	for _, terminalName := range []string{"", "xterm-256color", "linux", "vt100-color", "vt220-color"} {
		t.Run("color-"+terminalName, func(t *testing.T) {
			t.Setenv("TERM", terminalName)
			t.Setenv("NO_COLOR", "")
			if !New(slave, nil).formatter.ForceColors {
				t.Fatal("color-capable or unknown terminal was excluded")
			}
			t.Setenv("NO_COLOR", "1")
			if New(slave, nil).formatter.ForceColors {
				t.Fatal("NO_COLOR did not disable automatic colors")
			}
		})
	}
}
