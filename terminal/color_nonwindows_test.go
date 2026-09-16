//go:build !windows

package terminal

import (
	"bufio"
	"os"
	"testing"
)

func TestAutoColorsTerminal(test *testing.T) {
	file, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		test.Skipf("no controlling terminal: %v", err)
	}
	defer file.Close()
	for _, testCase := range []struct {
		name    string
		noColor string
		term    string
		want    bool
	}{
		{name: "terminal", term: "xterm-256color", want: true},
		{name: "no_color", term: "xterm-256color", noColor: "1"},
		{name: "no_color_zero_is_nonempty", term: "xterm-256color", noColor: "0"},
		{name: "dumb_terminal", term: "dumb"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Setenv("NO_COLOR", testCase.noColor)
			test.Setenv("TERM", testCase.term)
			if got := autoColors(file); got != testCase.want {
				test.Fatalf("automatic colors = %v, want %v", got, testCase.want)
			}
			output := New(file, nil)
			if output.formatter.DisableColors != !testCase.want {
				test.Fatal("output did not use automatic color detection")
			}
			if autoColors(bufio.NewWriter(file)) {
				test.Fatal("automatic colors enabled for an unknown wrapper")
			}
		})
	}
}
