package terminal

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestAutoColorsRejectsNonTerminals(test *testing.T) {
	test.Setenv("NO_COLOR", "")
	test.Setenv("TERM", "xterm-256color")
	file, err := os.CreateTemp(test.TempDir(), "output")
	if err != nil {
		test.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	var nilFile *os.File
	for _, testCase := range []struct {
		name   string
		writer io.Writer
	}{
		{name: "nil"},
		{name: "typed_nil_file", writer: nilFile},
		{name: "buffer", writer: &bytes.Buffer{}},
		{name: "discard", writer: io.Discard},
		{name: "file", writer: file},
		{name: "pipe", writer: writer},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			if autoColors(testCase.writer) {
				test.Fatal("automatic colors enabled for a non-terminal writer")
			}
		})
	}
	if err := file.Close(); err != nil {
		test.Fatal(err)
	}
	if autoColors(file) {
		test.Fatal("automatic colors enabled for a closed file")
	}
}
