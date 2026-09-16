package easylog_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	easylog "github.com/LazyEasyDev/EasyLog"
	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

var (
	_ easylog.Output = (*fileoutput.Output)(nil)
	_ easylog.Output = (*terminal.Output)(nil)
)

func TestTerminalLifecyclePreservesCallerWriter(test *testing.T) {
	writer := &callerOwnedWriter{}
	var output easylog.Output = terminal.New(writer, nil)
	runtime := easylog.New(easylog.Options{}, []easylog.Output{output})
	runtime.Logger().Info("event")
	if err := runtime.Sync(); err != nil {
		test.Fatal(err)
	}
	for range 2 {
		if err := runtime.Close(); err != nil {
			test.Fatal(err)
		}
	}
	if writer.syncCalls != 0 || writer.closeCalls != 0 {
		test.Fatalf("caller-owned writer: sync calls=%d close calls=%d, want neither", writer.syncCalls, writer.closeCalls)
	}
}

func TestRuntimeCopiesOutputSliceAndPreservesOrder(test *testing.T) {
	var events []string
	first := &recordingOutput{name: "first", events: &events}
	second := &recordingOutput{name: "second", events: &events}
	third := &recordingOutput{name: "third", events: &events}
	replacement := &recordingOutput{name: "replacement", events: &events}
	outputs := []easylog.Output{first, nil, second, third}
	runtime := easylog.New(easylog.Options{MemoryMaxBytes: 1024}, outputs)
	test.Cleanup(func() { _ = runtime.Close() })
	for index := range outputs {
		outputs[index] = replacement
	}

	runtime.Logger().Info("event", "answer", 42)
	if err := runtime.Sync(); err != nil {
		test.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		test.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		test.Fatal(err)
	}
	want := []string{
		"first.write", "second.write", "third.write",
		"first.sync", "second.sync", "third.sync",
		"first.close", "second.close", "third.close",
	}
	if !slices.Equal(events, want) {
		test.Fatalf("output calls = %v, want %v", events, want)
	}
	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 1 {
		test.Fatalf("retained records=%d error=%v, want one record", len(records), err)
	}
	for _, output := range []*recordingOutput{first, second, third} {
		if len(output.records) != 1 {
			test.Fatalf("%s received %d records, want one", output.name, len(output.records))
		}
		if output.records[0].Level() != slog.LevelInfo || !bytes.Equal(output.records[0].JSON(), records[0].JSON()) {
			test.Fatalf("%s received different encoded data", output.name)
		}
	}
}

func TestRuntimeAttemptsAllOutputsOnErrors(test *testing.T) {
	for _, operation := range []string{"write", "sync", "close"} {
		test.Run(operation, func(test *testing.T) {
			firstErr := errors.New("first output failed")
			secondErr := errors.New("second output failed")
			var events []string
			first := &recordingOutput{
				name: "first", events: &events,
				writeErr: firstErr, syncErr: firstErr, closeErr: firstErr,
			}
			second := &recordingOutput{
				name: "second", events: &events,
				writeErr: secondErr, syncErr: secondErr, closeErr: secondErr,
			}
			third := &recordingOutput{name: "third", events: &events}
			runtime := easylog.New(easylog.Options{MemoryMaxBytes: 1024}, []easylog.Output{first, nil, second, third})
			test.Cleanup(func() { _ = runtime.Close() })
			var err error
			switch operation {
			case "write":
				record := slog.NewRecord(time.Now(), slog.LevelInfo, "event", 0)
				err = runtime.Handler().Handle(context.Background(), record)
				if runtime.Consumer().Len() != 1 {
					test.Fatal("output failures prevented memory retention")
				}
			case "sync":
				err = runtime.Sync()
			case "close":
				err = runtime.Close()
				if duplicateErr := runtime.Close(); duplicateErr != nil {
					test.Fatalf("duplicate Close returned an error: %v", duplicateErr)
				}
			}
			if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
				test.Fatalf("joined error = %v, want both output failures", err)
			}
			want := []string{"first." + operation, "second." + operation, "third." + operation}
			if !slices.Equal(events, want) {
				test.Fatalf("output calls = %v, want %v", events, want)
			}
		})
	}
}

func TestRuntimeClosedState(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		outputs []easylog.Output
	}{
		{name: "nil_slice"},
		{name: "empty_slice", outputs: []easylog.Output{}},
		{name: "nil_entries", outputs: []easylog.Output{nil, nil}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			enrichCalls := 0
			runtime := easylog.New(easylog.Options{
				MemoryMaxBytes: 1024,
				Enrichers: []easylog.Enricher{func(context.Context) []slog.Attr {
					enrichCalls++
					return nil
				}},
			}, testCase.outputs)
			logger := runtime.Logger()
			derived := logger.With("service", "api").WithGroup("request")
			logger.Info("retained")
			if err := runtime.Sync(); err != nil {
				test.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				test.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				test.Fatal(err)
			}
			for _, logger := range []*slog.Logger{logger, derived} {
				if logger.Enabled(context.Background(), slog.LevelInfo) {
					test.Fatal("closed runtime still enables logging")
				}
				logger.Info("after close")
				record := slog.NewRecord(time.Now(), slog.LevelInfo, "direct handle", 0)
				if err := logger.Handler().Handle(context.Background(), record); !errors.Is(err, easylog.ErrClosed) {
					test.Fatalf("Handle after Close = %v, want ErrClosed", err)
				}
			}
			if err := runtime.Sync(); !errors.Is(err, easylog.ErrClosed) {
				test.Fatalf("Sync after Close = %v, want ErrClosed", err)
			}
			if enrichCalls != 1 || runtime.Consumer().Len() != 1 {
				test.Fatalf("enricher calls=%d retained records=%d, want one each", enrichCalls, runtime.Consumer().Len())
			}
		})
	}
}

type recordingOutput struct {
	name     string
	events   *[]string
	records  []easylog.Record
	writeErr error
	syncErr  error
	closeErr error
}

func (output *recordingOutput) WriteRecord(record easylog.Record) error {
	*output.events = append(*output.events, output.name+".write")
	output.records = append(output.records, record)
	return output.writeErr
}

func (output *recordingOutput) Sync() error {
	*output.events = append(*output.events, output.name+".sync")
	return output.syncErr
}

func (output *recordingOutput) Close() error {
	*output.events = append(*output.events, output.name+".close")
	return output.closeErr
}

type callerOwnedWriter struct {
	bytes.Buffer
	syncCalls  int
	closeCalls int
}

func (writer *callerOwnedWriter) Sync() error {
	writer.syncCalls++
	return nil
}

func (writer *callerOwnedWriter) Close() error {
	writer.closeCalls++
	return nil
}
