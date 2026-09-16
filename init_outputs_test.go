package easylog_test

import (
	"bytes"
	"log/slog"
	"slices"
	"testing"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func TestInitWithOutputsInstallsDefaultAndLifecycle(test *testing.T) {
	previous := slog.Default()
	var events []string
	first := &recordingOutput{name: "first", events: &events}
	second := &recordingOutput{name: "second", events: &events}
	replacement := &recordingOutput{name: "replacement", events: &events}
	outputs := []easylog.Output{first, nil, second}
	if err := easylog.InitWithOutputs(easylog.Options{
		Level: slog.LevelDebug, MemoryMaxBytes: 1024,
	}, outputs); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := easylog.Close(); err != nil {
			test.Error(err)
		}
	})
	consumer := easylog.Consumer()
	if slog.Default() == previous || consumer == nil {
		test.Fatal("InitWithOutputs did not install the logger and configured consumer")
	}
	if len(events) != 0 {
		test.Fatalf("initialization invoked output methods: %v", events)
	}
	for index := range outputs {
		outputs[index] = replacement
	}

	slog.Debug("event", "answer", 42)
	if err := easylog.Sync(); err != nil {
		test.Fatal(err)
	}
	if err := easylog.Close(); err != nil {
		test.Fatal(err)
	}
	if slog.Default() != previous || easylog.Consumer() != nil {
		test.Fatal("Close did not restore the logger and detach the consumer")
	}
	if err := easylog.Close(); err != nil {
		test.Fatal(err)
	}
	want := []string{
		"first.write", "second.write",
		"first.sync", "second.sync",
		"first.close", "second.close",
	}
	if !slices.Equal(events, want) {
		test.Fatalf("output calls = %v, want %v", events, want)
	}
	records, err := consumer.Take(0)
	if err != nil || len(records) != 1 {
		test.Fatalf("memory records=%d error=%v, want one record", len(records), err)
	}
	for _, output := range []*recordingOutput{first, second} {
		if len(output.records) != 1 {
			test.Fatalf("%s received %d records, want one", output.name, len(output.records))
		}
		if output.records[0].Level() != slog.LevelDebug || !bytes.Equal(output.records[0].JSON(), records[0].JSON()) {
			test.Fatalf("%s received different encoded data or ignored the configured level", output.name)
		}
	}
}

func TestInitWithOutputsAllowsNoDestinations(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		outputs []easylog.Output
	}{
		{name: "nil_slice"},
		{name: "empty_slice", outputs: []easylog.Output{}},
		{name: "nil_entry", outputs: []easylog.Output{nil}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			if err := easylog.InitWithOutputs(easylog.Options{MemoryMaxBytes: 1024}, testCase.outputs); err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				if err := easylog.Close(); err != nil {
					test.Error(err)
				}
			})
			slog.Info("retained")
			consumer := easylog.Consumer()
			if consumer == nil || consumer.Len() != 1 {
				test.Fatal("memory retention did not work without external outputs")
			}
			if err := easylog.Sync(); err != nil {
				test.Fatal(err)
			}
		})
	}
}
