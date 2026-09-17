package easylog

import (
	"bytes"
	"errors"
	"log"
	"log/slog"
	"testing"

	"github.com/LazyEasyDev/EasyLog/terminal"
)

var builtinSlogLoggerForTest = slog.Default()

func TestCloseRestoresBuiltinLogging(test *testing.T) {
	for _, testCase := range []struct {
		name       string
		initialize func() error
	}{
		{name: "Init", initialize: func() error { return Init(InitOptions{}) }},
		{name: "InitWithOutputs", initialize: func() error { return InitWithOutputs(Options{}, nil) }},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			preserveGlobalLoggingForTest(test)
			var destination bytes.Buffer
			slog.SetDefault(builtinSlogLoggerForTest)
			log.SetOutput(&destination)
			wantFlags := log.Ldate | log.Ltime | log.Lshortfile
			log.SetFlags(wantFlags)
			for range 2 {
				destination.Reset()
				if err := testCase.initialize(); err != nil {
					test.Fatal(err)
				}
				log.Print("legacy during EasyLog")
				slog.Info("slog during EasyLog")
				if destination.Len() != 0 {
					test.Fatalf("logging bypassed EasyLog: %q", destination.String())
				}
				if err := Close(); err != nil {
					test.Fatal(err)
				}
				if slog.Default() != builtinSlogLoggerForTest {
					test.Fatal("Close did not restore the built-in slog logger")
				}

				log.Print("legacy after close")
				slog.Info("slog after close")
				for _, message := range []string{"legacy after close", "slog after close"} {
					if !bytes.Contains(destination.Bytes(), []byte(message)) {
						test.Errorf("restored output = %q, missing %q", destination.String(), message)
					}
				}
				if log.Writer() != &destination {
					test.Error("Close did not restore the standard log writer")
				}
				if log.Flags() != wantFlags {
					test.Errorf("standard log flags = %d, want %d", log.Flags(), wantFlags)
				}
			}
		})
	}
}

func TestCloseRestoresCustomLogging(test *testing.T) {
	for _, testCase := range []struct {
		name                 string
		easyLog              bool
		separateLegacyWriter bool
	}{
		{name: "custom_handler"},
		{name: "custom_handler_with_separate_legacy_writer", separateLegacyWriter: true},
		{name: "independent_easylog", easyLog: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			preserveGlobalLoggingForTest(test)
			var structuredData, legacyData bytes.Buffer
			previous := slog.New(slog.NewJSONHandler(&structuredData, nil))
			if testCase.easyLog {
				earlier := New(Options{}, []Output{terminal.NewJSON(&structuredData)})
				previous = earlier.Logger()
				test.Cleanup(func() {
					if err := earlier.Close(); err != nil {
						test.Error(err)
					}
				})
			}
			slog.SetDefault(previous)
			legacyDestination := &structuredData
			if testCase.separateLegacyWriter {
				log.SetOutput(&legacyData)
				legacyDestination = &legacyData
			}
			previousWriter := log.Writer()
			wantFlags := log.Lshortfile | log.LUTC
			log.SetFlags(wantFlags)
			if err := InitWithOutputs(Options{}, nil); err != nil {
				test.Fatal(err)
			}
			if err := Close(); err != nil {
				test.Fatal(err)
			}
			if slog.Default() != previous || log.Writer() != previousWriter || log.Flags() != wantFlags {
				test.Fatal("Close did not restore the previous logging configuration")
			}
			slog.Info("structured restored")
			log.Print("legacy restored")
			if !bytes.Contains(structuredData.Bytes(), []byte("structured restored")) {
				test.Fatalf("previous slog logger did not receive the record: %q", structuredData.String())
			}
			if !bytes.Contains(legacyDestination.Bytes(), []byte("legacy restored")) {
				test.Fatalf("previous log writer did not receive the record: %q", legacyDestination.String())
			}
			if testCase.separateLegacyWriter && bytes.Contains(structuredData.Bytes(), []byte("legacy restored")) {
				test.Fatal("Close redirected legacy logging to the previous slog handler instead of its saved writer")
			}
		})
	}
}

func TestClosePreservesReplacementLogging(test *testing.T) {
	preserveGlobalLoggingForTest(test)
	if err := Init(InitOptions{}); err != nil {
		test.Fatal(err)
	}
	var structuredData, legacyData bytes.Buffer
	replacement := slog.New(slog.NewJSONHandler(&structuredData, nil))
	slog.SetDefault(replacement)
	log.SetOutput(&legacyData)
	wantFlags := log.Ldate | log.Lshortfile
	log.SetFlags(wantFlags)
	for range 2 {
		if err := Close(); err != nil {
			test.Fatal(err)
		}
		if slog.Default() != replacement || log.Writer() != &legacyData || log.Flags() != wantFlags {
			test.Fatal("Close changed the application's replacement logging configuration")
		}
	}
	slog.Info("replacement slog")
	log.Print("replacement log")
	if !bytes.Contains(structuredData.Bytes(), []byte("replacement slog")) || !bytes.Contains(legacyData.Bytes(), []byte("replacement log")) {
		test.Fatalf("replacement logging failed: slog=%q log=%q", structuredData.String(), legacyData.String())
	}
}

func TestCloseRestoresLoggingBeforeOutputClose(test *testing.T) {
	preserveGlobalLoggingForTest(test)
	var destination bytes.Buffer
	slog.SetDefault(builtinSlogLoggerForTest)
	log.SetOutput(&destination)
	wantFlags := log.Lshortfile
	log.SetFlags(wantFlags)
	closeFailure := errors.New("output close failed")
	output := &lifecycleTestOutput{closeOutput: func() error {
		if slog.Default() != builtinSlogLoggerForTest || log.Writer() != &destination || log.Flags() != wantFlags {
			test.Error("output Close ran before logging was restored")
		}
		log.Print("legacy during output close")
		slog.Info("slog during output close")
		return closeFailure
	}}
	if err := InitWithOutputs(Options{}, []Output{output}); err != nil {
		test.Fatal(err)
	}
	if err := Close(); !errors.Is(err, closeFailure) {
		test.Fatalf("Close error = %v, want %v", err, closeFailure)
	}
	for _, message := range []string{"legacy during output close", "slog during output close"} {
		if !bytes.Contains(destination.Bytes(), []byte(message)) {
			test.Errorf("restored output = %q, missing %q", destination.String(), message)
		}
	}
}

func preserveGlobalLoggingForTest(test *testing.T) {
	test.Helper()
	originalLogger := slog.Default()
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	test.Cleanup(func() {
		if err := Close(); err != nil {
			test.Error(err)
		}
		slog.SetDefault(originalLogger)
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
}
