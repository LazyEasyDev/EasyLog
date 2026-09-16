package easylog

import (
	"context"
	"errors"
	"log/slog"
	goruntime "runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestRuntimeSyncWaitsForWrite(test *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	test.Cleanup(unblock)
	syncCalled := make(chan struct{}, 1)
	output := &lifecycleTestOutput{
		writeRecord: func(Record) error {
			close(entered)
			<-release
			return nil
		},
		syncOutput: func() error {
			syncCalled <- struct{}{}
			return nil
		},
	}
	runtime := New(Options{}, []Output{output})
	written := make(chan error, 1)
	go func() {
		record := slog.NewRecord(time.Now(), slog.LevelInfo, "event", 0)
		written <- runtime.Handler().Handle(context.Background(), record)
	}()
	<-entered
	synced := make(chan error, 1)
	go func() { synced <- runtime.Sync() }()
	select {
	case err := <-synced:
		test.Fatalf("Sync returned before the active write finished: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-syncCalled:
		test.Fatal("output Sync overlapped an active write")
	default:
	}
	unblock()
	if err := <-written; err != nil {
		test.Fatal(err)
	}
	if err := <-synced; err != nil {
		test.Fatal(err)
	}
	select {
	case <-syncCalled:
	default:
		test.Fatal("output Sync was not called")
	}
	if err := runtime.Close(); err != nil {
		test.Fatal(err)
	}
}

func TestRuntimeCloseWaitsForSyncAndDuplicatesReturn(test *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	test.Cleanup(unblock)
	closeCalled := make(chan struct{}, 1)
	var events []string
	first := &lifecycleTestOutput{
		syncOutput: func() error {
			events = append(events, "first.sync")
			close(entered)
			<-release
			return nil
		},
		closeOutput: func() error {
			events = append(events, "first.close")
			closeCalled <- struct{}{}
			return nil
		},
	}
	second := &lifecycleTestOutput{
		syncOutput: func() error {
			events = append(events, "second.sync")
			return nil
		},
		closeOutput: func() error {
			events = append(events, "second.close")
			return nil
		},
	}
	runtime := New(Options{}, []Output{first, second})
	synced := make(chan error, 1)
	go func() { synced <- runtime.Sync() }()
	finishSync := sync.OnceValue(func() error {
		unblock()
		return <-synced
	})
	test.Cleanup(func() {
		if err := finishSync(); err != nil {
			test.Error(err)
		}
	})
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- runtime.Close() }()
	finishClose := sync.OnceValue(func() error {
		unblock()
		return <-closed
	})
	test.Cleanup(func() {
		if err := finishClose(); err != nil {
			test.Error(err)
		}
	})
	deadline := time.Now().Add(time.Second)
	for !runtime.state.closed.Load() {
		if time.Now().After(deadline) {
			test.Fatal("runtime shutdown did not start")
		}
		goruntime.Gosched()
	}
	duplicate := make(chan error, 1)
	go func() { duplicate <- runtime.Close() }()
	select {
	case err := <-duplicate:
		if err != nil {
			test.Fatalf("duplicate Close: %v", err)
		}
	case <-time.After(time.Second):
		test.Fatal("duplicate Close waited for active output I/O")
	}
	select {
	case <-closeCalled:
		test.Fatal("output Close overlapped active Sync")
	default:
	}
	if err := finishSync(); err != nil {
		test.Fatal(err)
	}
	if err := finishClose(); err != nil {
		test.Fatal(err)
	}
	want := []string{"first.sync", "second.sync", "first.close", "second.close"}
	if !slices.Equal(events, want) {
		test.Fatalf("output calls = %v, want %v", events, want)
	}
}

func TestRuntimeLateEncodingCannotWriteAfterClose(test *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	test.Cleanup(unblock)
	writes := 0
	closes := 0
	output := &lifecycleTestOutput{
		writeRecord: func(Record) error {
			writes++
			return nil
		},
		closeOutput: func() error {
			closes++
			return nil
		},
	}
	runtime := New(Options{
		Enrichers: []Enricher{func(context.Context) []slog.Attr {
			close(entered)
			<-release
			return nil
		}},
	}, []Output{output})
	handled := make(chan error, 1)
	go func() {
		record := slog.NewRecord(time.Now(), slog.LevelInfo, "late record", 0)
		handled <- runtime.Handler().Handle(context.Background(), record)
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- runtime.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(time.Second):
		test.Fatal("Close waited for an enricher outside output I/O")
	}
	unblock()
	if err := <-handled; !errors.Is(err, ErrClosed) {
		test.Fatalf("late Handle = %v, want ErrClosed", err)
	}
	if writes != 0 || closes != 1 {
		test.Fatalf("output writes=%d closes=%d, want zero writes and one close", writes, closes)
	}
}

type lifecycleTestOutput struct {
	writeRecord func(Record) error
	syncOutput  func() error
	closeOutput func() error
}

func (output *lifecycleTestOutput) WriteRecord(record Record) error {
	if output.writeRecord != nil {
		return output.writeRecord(record)
	}
	return nil
}

func (output *lifecycleTestOutput) Sync() error {
	if output.syncOutput != nil {
		return output.syncOutput()
	}
	return nil
}

func (output *lifecycleTestOutput) Close() error {
	if output.closeOutput != nil {
		return output.closeOutput()
	}
	return nil
}
