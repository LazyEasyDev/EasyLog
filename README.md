# EasyLog

EasyLog is a small synchronous backend for Go's
[`log/slog`](https://pkg.go.dev/log/slog). It writes structured JSON logs to the
terminal, rotating files, and an optional bounded memory queue.

## Quick Start

```go
package main

import (
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func main() {
	logDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	if err := easylog.Init(
		easylog.Options{},
		&easylog.FileOptions{Directory: logDirectory},
		os.Stdout,
	); err != nil {
		panic(err)
	}
	defer easylog.Close()

	slog.Info("application started", "address", ":8080")
}
```

`Init` installs EasyLog as `slog.Default`. The example writes to stdout and to
`<current-directory>/logs`. `FileOptions.Directory` must be an absolute path;
`os.Getwd()` returns one.

Pass `nil` for an output you do not need:

```go
easylog.Init(easylog.Options{}, nil, os.Stdout) // terminal only
easylog.Init(easylog.Options{}, &easylog.FileOptions{Directory: logDirectory}, nil) // file only
```

Call `easylog.Close()` before the application exits. It waits for active writes,
closes the managed file output, and restores the previous default logger.

`Init` returns `ErrAlreadyInitialized` while initialized or closing. A concurrent
`Close` returns `nil` immediately without waiting; only the call that starts
shutdown waits for output and returns any file-close error. Wait for that call
to finish before reinitializing. A blocked output writer can delay shutdown
indefinitely; `Close` has no timeout.

## Options

The zero-value `Options` logs at `INFO` and above with memory retention disabled.

```go
easylog.Options{
	Level:          slog.LevelDebug,
	AddSource:      true,
	MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
}
```

`DefaultMemoryMaxBytes` is 8 MiB. A positive value enables memory retention;
zero or a negative value disables it. `ReplaceAttr` customizes encoded
attributes, and `Enrichers` can add values derived from `context.Context`.

## Memory Queue

When memory retention is enabled, `Consumer` provides destructive FIFO reads:

```go
consumer := easylog.Consumer()
if consumer != nil {
	records, err := consumer.Take(100)
	if err != nil {
		return err
	}
	for _, record := range records {
		process(record.JSON())
	}
}
```

`Take` returns immediately. `Take(0)` drains all available records. When the
limit is reached, the oldest records are evicted; a record larger than the
entire limit is not retained. Terminal and file output are unaffected.

## File Output

EasyLog creates a `logs` subdirectory beneath `FileOptions.Directory`.

- Files contain newline-delimited JSON.
- Levels use `debug_`, `info_`, `warn_`, and `err_` prefixes.
- Names use a UTC date and sequence, such as `info_20260914_0.jsonl`.
- Segments rotate at 8 MiB or when the UTC date changes.
- Seven segments per level are retained by default.
- A restart resumes the latest usable segment for the current UTC date.

If a required segment cannot be created, that record is dropped from file output
and `WriteRecord` returns the creation error. It is not appended to the old
segment or queued for replay. Later records retry creation; terminal output and
memory retention are unaffected.

Set `MaxSegmentBytes`, `MaxSegments`, or `Permissions` in `FileOptions` to
override the file defaults. Use only one file output or process for a managed
directory; multiple writers to the same directory are not coordinated.

## Independent Runtime

Use `New` when you do not want to replace `slog.Default`:

```go
files, err := fileoutput.New(fileoutput.Options{Directory: logDirectory})
if err != nil {
	return err
}
defer files.Close()

runtime := easylog.New(easylog.Options{}, easylog.Outputs{
	Terminal: terminal.New(os.Stderr),
	File:     files,
})
logger := runtime.Logger()
logger.Info("application started")
```

The caller owns outputs passed to `New`. `Runtime.Handler()` is also available
for constructing a custom `slog.Logger`.

## Behavior

- Output is synchronous and serialized: terminal first, then file.
- Every configured output is attempted even if another output fails.
- Standard `slog.Info` and similar calls do not return output errors.
- `file.Output.Sync()` explicitly flushes active files to stable storage.

Run the example and tests with:

```sh
go run ./main
go test -race ./...
```