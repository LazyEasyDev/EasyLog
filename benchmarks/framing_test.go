package benchmarks

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LazyEasyDev/EasyLog/terminal"
)

const framingFileLimit = 32 << 20

var framingSizes = []int{128, 1024, 4096}

type framingOutput interface {
	WriteRecord(slog.Record, []byte) error
}

type splitFramingOutput struct {
	mu      sync.Mutex
	writer  io.Writer
	newline [1]byte
}

func (output *splitFramingOutput) WriteRecord(_ slog.Record, content []byte) error {
	output.mu.Lock()
	defer output.mu.Unlock()
	if err := writeFramingBytes(output.writer, content); err != nil {
		return err
	}
	return writeFramingBytes(output.writer, output.newline[:])
}

func writeFramingBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func newFramingOutput(test testing.TB, writer io.Writer, variant string) framingOutput {
	test.Helper()
	switch variant {
	case "OneWrite":
		return terminal.NewJSON(writer)
	case "TwoWrites":
		return &splitFramingOutput{writer: writer, newline: [1]byte{'\n'}}
	default:
		test.Fatalf("unknown framing variant %q", variant)
		return nil
	}
}

func framingContent(test testing.TB, size int) []byte {
	test.Helper()
	content, err := json.Marshal(struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
	}{
		Level:   "INFO",
		Message: strings.Repeat("x", size-len(`{"level":"INFO","msg":""}`)),
	})
	if err != nil {
		test.Fatal(err)
	}
	if len(content) != size {
		test.Fatalf("JSON size = %d, want %d", len(content), size)
	}
	return content
}

func openFramingDestination(test testing.TB, destination string) *os.File {
	test.Helper()
	name := os.DevNull
	flags := os.O_WRONLY
	if destination == "FileAppend" {
		name = filepath.Join(test.TempDir(), "framing.jsonl")
		flags |= os.O_CREATE | os.O_EXCL | os.O_APPEND
	}
	file, err := os.OpenFile(name, flags, 0o600)
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := file.Close(); err != nil {
			test.Error(err)
		}
	})
	return file
}

func BenchmarkFraming(bench *testing.B) {
	for _, destination := range []string{"DevNull", "FileAppend"} {
		bench.Run("sink="+destination, func(bench *testing.B) {
			for _, size := range framingSizes {
				bench.Run("bytes="+strconv.Itoa(size), func(bench *testing.B) {
					content := framingContent(bench, size)
					for _, variant := range []string{"OneWrite", "TwoWrites"} {
						bench.Run("mode="+variant, func(bench *testing.B) {
							file := openFramingDestination(bench, destination)
							output := newFramingOutput(bench, file, variant)
							if err := output.WriteRecord(slog.Record{}, content); err != nil {
								bench.Fatal(err)
							}
							lineBytes := int64(len(content) + 1)
							retainedBytes := lineBytes
							bench.ReportAllocs()
							bench.SetBytes(lineBytes)
							for bench.Loop() {
								if destination == "FileAppend" && retainedBytes+lineBytes > framingFileLimit {
									bench.StopTimer()
									if err := file.Truncate(0); err != nil {
										bench.Fatal(err)
									}
									retainedBytes = 0
									bench.StartTimer()
								}
								if err := output.WriteRecord(slog.Record{}, content); err != nil {
									bench.Fatal(err)
								}
								retainedBytes += lineBytes
							}
							if destination == "FileAppend" {
								info, err := file.Stat()
								if err != nil {
									bench.Fatal(err)
								}
								if info.Size() != retainedBytes {
									bench.Fatalf("file size = %d, want %d", info.Size(), retainedBytes)
								}
							}
						})
					}
				})
			}
		})
	}
}

func TestFramingVariants(test *testing.T) {
	for _, size := range framingSizes {
		for _, variant := range []string{"OneWrite", "TwoWrites"} {
			test.Run(strconv.Itoa(size)+"/"+variant, func(test *testing.T) {
				content := framingContent(test, size)
				storage := append(bytes.Clone(content), '!')
				content = storage[:len(content)]
				original := bytes.Clone(storage)
				writer := &framingCountWriter{}
				output := newFramingOutput(test, writer, variant)
				file := openFramingDestination(test, "FileAppend")
				fileOutput := newFramingOutput(test, file, variant)
				for range 3 {
					if err := output.WriteRecord(slog.Record{}, content); err != nil {
						test.Fatal(err)
					}
					if err := fileOutput.WriteRecord(slog.Record{}, content); err != nil {
						test.Fatal(err)
					}
				}
				wantCalls := 3
				if variant == "TwoWrites" {
					wantCalls *= 2
				}
				want := bytes.Repeat([]byte(string(content)+"\n"), 3)
				if writer.calls != wantCalls || !bytes.Equal(writer.Bytes(), want) {
					test.Fatalf("calls=%d bytes=%d, want %d calls and %d bytes", writer.calls, writer.Len(), wantCalls, len(want))
				}
				data, err := os.ReadFile(file.Name())
				if err != nil {
					test.Fatal(err)
				}
				if !bytes.Equal(data, want) || !bytes.Equal(storage, original) {
					test.Fatal("file contents differ or framing modified the input")
				}
			})
		}
	}
}

type framingCountWriter struct {
	bytes.Buffer
	calls int
}

func (writer *framingCountWriter) Write(data []byte) (int, error) {
	writer.calls++
	return writer.Buffer.Write(data)
}
