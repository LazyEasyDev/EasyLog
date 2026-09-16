# EasyLog

EasyLog is a small synchronous backend for Go's
[`log/slog`](https://pkg.go.dev/log/slog). It encodes structured JSON once and
delivers it to configurable outputs and an optional bounded memory queue.
Built-in outputs render readable terminal text and write rotating JSON files.
Explicit NDJSON output to a writer is also available.

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

	if err := easylog.Init(easylog.InitOptions{
		File:     &easylog.FileOptions{Directory: logDirectory},
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
	}); err != nil {
		panic(err)
	}
	defer easylog.Close()

	slog.Info("application started", "address", ":8080")
}
```

`Init` installs EasyLog as `slog.Default`. The example writes default text to
stdout and JSON to `<current-directory>/logs`. `FileOptions.Directory` must be
an absolute path; `os.Getwd()` returns one.

Omit `File` or `Terminal` for an output you do not need. These are alternative
terminal-only and file-only setups:

```go
easylog.Init(easylog.InitOptions{
	Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
})

easylog.Init(easylog.InitOptions{
	File: &easylog.FileOptions{Directory: logDirectory},
})
```

A nil `Terminal.Writer` also disables terminal output, even when a formatter is
configured. It does not default to stderr. `InitOptions{}` installs a logger
without external outputs or memory retention.

Call `easylog.Close()` before the application exits. It waits for active output
I/O, closes the configured outputs, and restores the previous default logger.
The terminal output does not close its caller-owned writer. Call `easylog.Sync()`
explicitly when file data must be synchronized to stable storage; closing does
not imply synchronization.

`Init` and `InitWithOutputs` return `ErrAlreadyInitialized` while initialized or
closing. A concurrent `Close` returns `nil` immediately without waiting; only the
call that starts shutdown waits for output and returns any output-close errors.
Wait for that call to finish before reinitializing. A blocked output operation
can delay shutdown indefinitely; `Close` has no timeout.

## Options

`Init` takes a single `InitOptions` value. Its `Runtime` field holds the same
`Options` used by `New` and `InitWithOutputs`; `File` and `Terminal` configure
the built-in outputs. The zero-value runtime `Options` logs at `INFO` and above
with memory retention disabled.

```go
easylog.InitOptions{
	Runtime: easylog.Options{
		Level:          slog.LevelDebug,
		AddSource:      true,
		MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
	},
}
```

`New(options Options, outputs []Output)` and
`InitWithOutputs(options Options, outputs []Output)` are unchanged. Only `Init`
uses the grouped configuration instead of positional output arguments.

`DefaultMemoryMaxBytes` is 8 MiB. A positive value enables memory retention;
zero or a negative value disables it. `ReplaceAttr` customizes encoded
attributes, and `Enrichers` can add values derived from `context.Context`.

## Terminal Display

Terminal output uses text by default. A nil `Terminal.Formatter` or
`terminal.New(writer, nil)` creates an independent default formatter with
elapsed timestamps, a visible level, and automatic colors. A supplied formatter
uses its own settings; `&TerminalFormatter{}` therefore hides the level because
`ShowLevel` is false. Set a formatter pointer to customize the display:

```go
err := easylog.Init(easylog.InitOptions{
	File: &easylog.FileOptions{Directory: logDirectory},
	Terminal: &easylog.TerminalOptions{
		Writer: os.Stdout,
		Formatter: &easylog.TerminalFormatter{
			ForceColors:      false,
			DisableColors:    false,
			DisableTimestamp: false,
			TimestampFormat:  "15:04:05",
			ShowLevel:        true,
		},
	},
})
if err != nil {
	return err
}
defer easylog.Close()

slog.Info("application started", "address", ":8080")
```

The terminal displays a line such as:

```text
INFO[10:11:12] application started address=:8080
```

The level appears immediately before the timestamp without a `level=` label.
The standard prefixes are `DEBU`, `INFO`, `WARN`, and `ERRO` for four-letter
alignment; encoded JSON still uses `DEBUG` and `ERROR`.
The message is displayed directly, without `msg=` or surrounding quotes;
structured attributes remain `key=value` fields.

- `ForceColors: true` emits ANSI colors even for redirected or unknown writers,
	and overrides `NO_COLOR` and `TERM=dumb`.
- `DisableColors: true` removes ANSI colors and takes precedence over
	`ForceColors`, even when both flags are set.
- `DisableTimestamp: true` hides the timestamp and the top-level `time` field,
	regardless of `TimestampFormat`.
- `TimestampFormat: ""` shows elapsed seconds, such as `[0000]` or `[0012]`.
	Each terminal output starts its own monotonic timer when constructed by `Init`
	or `terminal.New`. Elapsed time is measured when the record is formatted,
	independently of the record's timestamp. It is padded to at least four digits
	and is unaffected by system-clock adjustments.
- A nonempty `TimestampFormat` is a Go time layout applied to the record's
	timestamp, such as `"15:04:05"` for `[10:11:12]`. Use `time.RFC3339` explicitly
	for the full date/time display; an empty layout now selects elapsed time.
- `ShowLevel: true` displays the top-level `level` value as the prefix; `false`
	hides it without changing level filtering. With timestamps disabled, the level
	is followed by a space and the message.

When both color flags are false, colors are detected once at output construction.
Automatic detection recognizes `*os.File` writers connected to a terminal on
macOS, Linux, and other supported Unix systems. On Windows, the console must
already have processed output and virtual terminal processing enabled; EasyLog
does not change console modes. A nonempty `NO_COLOR` or `TERM=dumb` disables
automatic colors. Redirected files, pipes, buffers, and unknown wrappers use
plain text. Forcing colors only emits ANSI bytes; it does not make an unsupported
destination render them or enable Windows console processing.

When colors are enabled, the level prefix and attribute names use severity
colors: gray for DEBUG, cyan for INFO, yellow for WARN, and red for ERROR.
Timestamps, messages, separators, and other values (including nested JSON) use
the terminal's default color. Field names remain colored when the level label
is hidden.

With an empty layout, the same text output starts like this:

```text
INFO[0000] application started address=:8080
```

File output and memory records always retain the original JSON, including the
full timestamp and level. Display attributes use the already-encoded fields, so
formatting does not evaluate attributes again. Nested objects and arrays remain
JSON within the text line; other fields keep their order, including duplicates.
Message spaces, quotes, and backslashes are preserved. Control characters in
messages and timestamp layouts are escaped to keep each record on one line;
structured string fields are still quoted when needed.

Record timestamp and level controls match the canonical `time` and `level` keys.
If `ReplaceAttr` renames them, the renamed fields are displayed as ordinary
attributes. In absolute-time mode, missing timestamps are not synthesized and
non-RFC3339 timestamp values are displayed unchanged. Elapsed mode always shows
its timer unless `DisableTimestamp` is true, even if the record has no timestamp.
Direct message rendering applies to strings under the canonical `msg` key;
renamed message keys and non-string replacements remain ordinary fields.

For an independent runtime, configure its terminal output directly:

```go
output := terminal.New(os.Stderr, &terminal.TextFormatter{
	DisableColors:    true,
	DisableTimestamp: true,
	ShowLevel:        false,
})
runtime := easylog.New(easylog.Options{}, []easylog.Output{output})
defer runtime.Close()
```

`terminal.New(writer, formatter)` accepts exactly one formatter pointer. `Init`
forwards its `Terminal.Formatter` to this constructor. Each output copies the
supplied formatter or creates its own default; color detection and later caller
mutations never modify another output's configuration. Changing the caller's
`TerminalOptions` fields does not reconfigure the installed output. The writer
itself remains caller-owned.

For newline-delimited JSON, use `terminal.NewJSON(writer)` explicitly:

```go
if err := easylog.InitWithOutputs(easylog.Options{}, []easylog.Output{
	terminal.NewJSON(os.Stdout),
}); err != nil {
	return err
}
defer easylog.Close()
```

`NewJSON` bypasses text formatting, preserves the encoded JSON, and appends one
newline per record. Both terminal constructors default a nil writer to
`os.Stderr`; `InitOptions.Terminal` with a nil writer still disables output.
Existing one-argument `terminal.New(writer)` calls must become
`terminal.New(writer, nil)` for default text or `terminal.NewJSON(writer)` to
keep their previous NDJSON behavior.

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

## Custom Initialization

Use `InitWithOutputs` to choose your own outputs while keeping the default slog
logger and package-level lifecycle:

```go
outputs := []easylog.Output{
	terminal.New(os.Stderr, nil),
	customOutput,
}
if err := easylog.InitWithOutputs(easylog.Options{
	Level:          slog.LevelDebug,
	MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
}, outputs); err != nil {
	return err
}
defer easylog.Close()

slog.Info("application started")
```

After success, `easylog.Sync()`, `easylog.Close()`, and `easylog.Consumer()` work
as they do with `Init`. Only the supplied outputs are configured, and the slice
is copied. A nil or empty slice supports a memory-only runtime; nil interface
entries are skipped, but typed-nil outputs are not supported.

Both initializers share the same package state and installation path. Rejected
initialization leaves the installed runtime, logger, and supplied outputs
untouched: it does not write, sync, or close those outputs. The caller remains
responsible for reusing or cleaning up newly created outputs after a rejection.
`Init` also checks the initialization guard before creating its built-in outputs.

Use `Init` for convenient terminal and file setup, `InitWithOutputs` for custom
package-level setup, or `New` for an independent runtime.

## Independent Runtime

Use `New` when you do not want to replace `slog.Default`:

```go
files, err := fileoutput.New(fileoutput.Options{Directory: logDirectory})
if err != nil {
	return err
}

runtime := easylog.New(easylog.Options{}, []easylog.Output{
	terminal.New(os.Stderr, nil),
	files,
})
defer runtime.Close()
logger := runtime.Logger()
logger.Info("application started")
```

`New` copies the slice, so changing the caller's slice does not change the
configured destinations. It does not copy the output objects. A nil or empty
slice disables external outputs without disabling configured memory retention.
Nil interface entries are skipped; typed-nil outputs are not supported.

The runtime coordinates `Sync` and `Close` for all supplied outputs. Each output
is responsible for its own resources; avoid closing it directly while its
runtime is active. Shared outputs require caller coordination of concurrency
and lifecycle. `Runtime.Handler()` remains available for standard slog handler
composition, and `New` never changes the default logger.

## Custom Outputs

Any type implementing this interface can be included in the output slice:

```go
type Output interface {
	WriteRecord(Record) error
	Sync() error
	Close() error
}
```

External implementations use `easylog.Record` as the `WriteRecord` parameter.
No internal-package imports or slog handler implementation are required.
`Record.Level()` provides severity, and `Record.JSON()` returns a copy of the
encoded JSON object without a trailing newline. Outputs may retain the record.

`Sync` flushes output-owned buffers and synchronizes storage where supported.
`Close` finishes pending work, releases only output-owned resources, and must
be safe to call more than once. The terminal output has no owned buffers or
resources, so its `Sync` and `Close` methods are no-ops. If its caller-provided
writer buffers data, the caller must flush and close that writer separately.

## Lifecycle

`Runtime.Sync()` and `Runtime.Close()` call the corresponding method on every
output in configured order, attempting all even when one fails. Errors are
joined and labeled with the zero-based output index; `errors.Is` can inspect
individual failures. The runtime serializes these operations with record writes.

`Close` marks the runtime closed before waiting for active output I/O. New
logging calls are disabled, and direct `Handler.Handle` or `Runtime.Sync` calls
return `ErrClosed`. An already active write or sync finishes its entire fan-out
before outputs are closed. Duplicate closes return nil immediately; only the
first call waits for shutdown and returns close errors. `Close` does not call
`Sync` automatically.

Stop and join log producers before syncing or closing. Enrichers and encoding
already in progress are not awaited by `Close`: their records may still reach
memory retention but cannot start output writes after shutdown begins. Retained
records remain available through an independent runtime's `Consumer()` after
close. Output methods must not reenter logging, `Sync`, or `Close` on the same
runtime.

The package-level `Sync()` delegates to the current runtime without holding the
package lock during output I/O. It returns nil when no runtime is installed; if
shutdown starts after it captures a runtime, it may return `ErrClosed`.
Package-level `Close()` additionally restores the previous default logger if
EasyLog's logger is still the default, and detaches the package consumer.

## Behavior

- Output is synchronous and serialized in slice order. `Init` configures terminal
  first, then file.
- Every configured output is attempted even if another output fails.
- Standard `slog.Info` and similar calls do not return output errors.
- Memory retention is independent of output success and is updated before output
  I/O; concurrent memory order can differ from serialized output order.
- `Runtime.Sync()` and `easylog.Sync()` include synchronization of active managed
  files to stable storage.

Run the example and tests with:

```sh
go run ./main
go test -race ./...
```