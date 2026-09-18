# EasyLog

Use Go's `log/slog` with readable terminal logs, rotating JSON files, custom outputs,
and optional in-memory retention. Use `Init` for a global logger or `New` for an
independent one. **Memory retention is off by default.**

Requires Go 1.25 or newer.

```sh
go get github.com/LazyEasyDev/EasyLog
```

[Getting started](#getting-started) | [Logging styles](#logging-styles) |
[Outputs](#outputs) | [Memory](#optional-memory) | [Options](#options) |
[Design and contracts](#design-and-contracts)

## Getting Started

### Global Logger: Init

Initialize once at startup, then use standard `slog` calls throughout your app.

```go
package main

import (
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func main() {
	if err := easylog.Init(easylog.InitOptions{
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
	}); err != nil {
		panic(err)
	}
	defer easylog.Close()

	slog.Info("application started", "address", ":8080")
	slog.With("service", "checkout").Warn("request retrying", "attempt", 2)
}
```

```text
INFO[0000] application started address=:8080
WARN[0000] request retrying service=checkout attempt=2
```

`Init` sets `slog.Default()` and also routes standard `log.Print` calls through
EasyLog. This example writes text to stdout without file output or memory retention.

### Independent Logger: New

Use `New` to pass a logger to a component, keep separate configurations, or avoid
changing the process-wide logger.

```go
package main

import (
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func main() {
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.NewJSON(os.Stdout),
	})
	defer runtime.Close()

	logger := runtime.Logger().With("service", "orders")
	logger.Info("order received", "order_id", "order-42")
}
```

This writes one JSON object per line, including `time`, `level`, `msg`, and your
fields. `runtime.Logger()` is a normal `*slog.Logger`; `runtime.Handler()` exposes
its `slog.Handler`. `New` leaves `slog.Default()` unchanged.

| Entry Point | Use When | Lifecycle |
| --- | --- | --- |
| `Init(InitOptions)` | Global logger with built-in terminal and/or file output | `easylog.Sync()`, `easylog.Close()` |
| `New(Options, []Output)` | Independent logger with explicit outputs | `runtime.Sync()`, `runtime.Close()` |
| `InitWithOutputs(Options, []Output)` | Global logger with explicit outputs | `easylog.Sync()`, `easylog.Close()` |

The short examples defer `Close`. See [Shutdown](#shutdown) for durability and
error handling, and [main/main.go](main/main.go) for complete examples.

### Run the Examples

From a checkout of this repository:

```sh
go run ./main
go run ./main -example new
go run ./main -example init -log-dir /tmp/easylog-demo
```

No files are created unless `-log-dir` is supplied. The `init` example then writes
under `<log-dir>/logs`.

| Example | Demonstrates |
| --- | --- |
| `init` | Global `slog`, standard `log`, all levels, optional rotating files |
| `new` | Independent JSON logger, structs, groups, typed attributes, source, redaction, dynamic level |
| `context` | Per-request fields through context enrichers |
| `memory` | Explicit memory retention, queue size, paging, draining |
| `outputs` | `InitWithOutputs`, text and JSON from the same event |
| `format` | JSON-to-text formatting without a logger |
| `all` | Every example in sequence; the default |

## Logging Styles

The recipes below use the imports and runtime shown above. Add standard packages
such as `context` and `log/slog` where used; complete code is in
[main/main.go](main/main.go).

### Fields and Groups

```go
logger := runtime.Logger()
logger.Info("order received", "order_id", "order-42", "items", 3)

service := logger.With("service", "checkout", "version", "1.0")
service.Info("ready", "address", ":8080")

payment := service.WithGroup("payment").With("provider", "sandbox")
payment.Info("authorized", "amount", 42.50)

logger.LogAttrs(context.Background(), slog.LevelInfo, "request completed",
	slog.Group("http", slog.String("method", "POST"), slog.Int("status", 201)),
	slog.Int("attempt", 1),
)
```

Use key/value pairs for convenience, `LogAttrs` for explicit types, and groups to
nest related fields. Structs and other JSON-compatible values can be field values.

**Bind stable fields with `With`; pass changing values on each log call.** Bound
fields, including `LogValuer` values, are prepared at `With` time. Later mutations
do not change that child's stored JSON, matching the standard JSON handler.

### Dynamic Levels

```go
var level slog.LevelVar
runtime := easylog.New(easylog.Options{Level: &level}, []easylog.Output{
	terminal.NewJSON(os.Stdout),
})
defer runtime.Close()

logger := runtime.Logger()
logger.Debug("hidden at the default INFO level")
level.Set(slog.LevelDebug)
logger.Debug("debug enabled")
```

For a fixed threshold, use `Level: slog.LevelDebug`. With `Init`, put these settings
in `InitOptions.Runtime`.

### Request Context

Enrichers extract fields from each enabled call's context. Use a private key type:

```go
type requestIDKey struct{}

runtime := easylog.New(easylog.Options{
	Enrichers: []easylog.Enricher{
		func(ctx context.Context) []slog.Attr {
			requestID, ok := ctx.Value(requestIDKey{}).(string)
			if !ok {
				return nil
			}
			return []slog.Attr{slog.String("request_id", requestID)}
		},
	},
}, []easylog.Output{terminal.NewJSON(os.Stdout)})
defer runtime.Close()

ctx := context.WithValue(context.Background(), requestIDKey{}, "req-123")
runtime.Logger().InfoContext(ctx, "request accepted", "path", "/orders")
```

`LogAttrs(ctx, ...)` also passes context to enrichers. Plain `Info(...)` does not
carry your request context.

### Field Customization

Pass these options to `New` or `InitWithOutputs`, or to `InitOptions.Runtime`:

```go
options := easylog.Options{
	AddSource: true,
	ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "token" {
			return slog.String(attr.Key, "[REDACTED]")
		}
		return attr
	},
}
```

`ReplaceAttr` can rename, transform, or remove custom fields; return `slog.Attr{}` to
remove one. It runs at binding for bound fields and per call for call-site fields.
Built-in `time`, `level`, `msg`, and `source` metadata never reaches the callback
and cannot be renamed, removed, or changed by it. The example redacts slog fields
named `token`, including inside named
slog groups. It does not inspect members of arbitrary structs or maps; redact those
values before logging them.

### Callback Safety

Avoid logging through the same runtime from `ReplaceAttr`, `Enrichers`, or
`LogValuer.LogValue` callbacks. Recursive logging can reenter those callbacks
indefinitely. Attribute resolution, replacement, and custom JSON marshaling run
during preparation, outside the JSON capture and output locks.
If the original call came through standard `log.Print`, calling
`log.Print` again inside a callback deadlocks on the standard logger's mutex,
before EasyLog receives the nested call. This also affects the standard slog
bridge; EasyLog cannot intercept the blocked call to reject it.

For callback diagnostics, create a separate logger once, outside callbacks, using
the standard `log` and `os` packages:

```go
diagnostic := log.New(os.Stderr, "easylog diagnostic: ", log.LstdFlags)
```

Inside a callback, use `diagnostic.Print("processing log attributes")` instead.
Keep its destination independent of EasyLog; writing directly to stderr bypasses
the runtime and uses a separate logger mutex. Do not redirect it back through
the same logging pipeline.

## Outputs

### Terminal Text and JSON

Import `github.com/LazyEasyDev/EasyLog/terminal`. Use `terminal.New(writer, nil)` for
default text or `terminal.NewJSON(writer)` for raw NDJSON. To customize text:

```go
formatter := terminal.GetDefaultTextFormatter()
formatter.TimestampFormat = "15:04:05"
formatter.DisableColors = true

runtime := easylog.New(easylog.Options{}, []easylog.Output{
	terminal.New(os.Stdout, &formatter),
})
defer runtime.Close()
runtime.Logger().Info("ready", "service", "api")
```

| Text Setting | Behavior |
| --- | --- |
| `TimestampFormat: ""` | Elapsed seconds since output creation; the default |
| `TimestampFormat: "15:04:05"` | Format record time using a Go layout |
| `DisableTimestamp: true` | Hide timestamps |
| `ShowLevel: true` | Display the level; does not change filtering |
| `DisableColors: true` | Disable colors, including forced colors |
| `ForceColors: true` | Force colors for an ANSI-capable destination |

Defaults include the level and automatic colors, respecting `NO_COLOR`, `TERM=dumb`,
and terminal detection. Supplied formatters are copied; their zero value hides the
level. Direct terminal constructors use stderr for nil writers. In contrast, `Init`
disables terminal output when `Terminal.Writer` is nil.

### Rotating Files

Set `File` alongside `Terminal` to send each event to both destinations:

```go
directory, err := os.Getwd()
if err != nil {
	panic(err)
}
if err := easylog.Init(easylog.InitOptions{
	File: &easylog.FileOptions{
		Directory:       directory,
		MaxSegmentBytes: 8 * 1024 * 1024,
		MaxSegments:     7,
	},
	Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
}); err != nil {
	panic(err)
}
defer easylog.Close()
slog.Info("written to terminal and file")
if err := easylog.Sync(); err != nil {
	panic(err)
}
```

`Directory` must be absolute. Files live under its `logs` subdirectory, with names
like `info_20260917_0.jsonl`. Defaults: 8 MiB segments, seven segments per level
(including active), and permissions `0644`. Rotation uses UTC dates and size;
retention is best effort, not a hard disk quota.

Missing directories are created with `0755`, subject to the process's `umask`.
Existing directories are reused without validating or changing their permissions
or ownership. New segment files use `Permissions` (`0644` by default), subject to
`umask`; resumed files keep their existing permissions.

For `New`, import `github.com/LazyEasyDev/EasyLog/file`, call
`file.New(file.Options{Directory: directory})`, check its error, and include the
result in the output slice. Use one output/process per managed directory.

### Multiple Outputs

`New` and `InitWithOutputs` accept the same output list. This installs a global
logger with text on stderr and JSON on stdout:

```go
if err := easylog.InitWithOutputs(easylog.Options{}, []easylog.Output{
	terminal.New(os.Stderr, nil),
	terminal.NewJSON(os.Stdout),
}); err != nil {
	panic(err)
}
defer easylog.Close()
slog.Info("inventory updated", "sku", "item-42")
```

Any `io.Writer`, such as a buffer or network writer, can receive JSON through
`terminal.NewJSON(writer)`. Implement [Output](#custom-outputs) to manage your own
destination's lifecycle.

## Optional Memory

`Options{}` creates **no memory store** and `Consumer()` returns nil. Set a positive
`MemoryMaxBytes` to enable retention. `DefaultMemoryMaxBytes` is a recommended 8 MiB
limit, not an automatically applied default.

```go
runtime := easylog.New(easylog.Options{
	MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
}, nil)
defer runtime.Close()

runtime.Logger().Info("job completed", "rows", 128)
consumer := runtime.Consumer()
page, err := consumer.Take(100)
if err != nil {
	panic(err)
}
for _, line := range page {
	if _, err := os.Stdout.Write(line); err != nil {
		panic(err)
	}
}
```

This retains records without live outputs. Memory can also run alongside any
outputs. For a global runtime, call `easylog.Consumer()` instead.

| Method | Behavior |
| --- | --- |
| `Take(n)`, `n > 0` | Remove and return up to `n` oldest records |
| `Take(0)` | Drain all retained records |
| `Take(n)`, `n < 0` | Return `ErrInvalidPageSize` without changing the queue |
| `Len()` | Count retained records |
| `Bytes()` | Count retained JSON bytes, including newlines |

`Take` is nonblocking; an empty queue returns `nil, nil`. Returned lines are
independent mutable copies. Oldest records are evicted to fit; oversized records
are skipped without evicting others. Limits count JSON bytes, not total heap usage.
The queue reuses ring-buffer slots and retains spare capacity until fully drained.
Join lines with `bytes.Join(page, nil)` for NDJSON; use `[]json.RawMessage` for JSON
arrays, since `[][]byte` marshals as base64 strings.

## Options

Use `Options` with `New` or `InitWithOutputs`, or through `InitOptions.Runtime`.

| Option | Default | Purpose |
| --- | --- | --- |
| `Level` | INFO | Fixed threshold or a `*slog.LevelVar` |
| `AddSource` | `false` | Include source function, file, and line |
| `ReplaceAttr` | nil | Customize resolved custom attributes; built-in metadata is protected |
| `Enrichers` | nil | Extract fields from each enabled call's context |
| `MemoryMaxBytes` | `0`, disabled | Positive values enable bounded retention |

Nil `InitOptions.File` or `InitOptions.Terminal` disables that output.
`InitOptions{}` and `New(Options{}, nil)` configure no outputs or memory.

## Shutdown

Stop and join producers, then `Sync`, then `Close`. **Close does not call Sync** or
stop application goroutines. Handle both errors even if syncing fails:

```go
syncErr := runtime.Sync()
closeErr := runtime.Close()
if err := errors.Join(syncErr, closeErr); err != nil {
	fmt.Fprintln(os.Stderr, err)
}
```

For global initialization, use `easylog.Sync()` and `easylog.Close()` instead.
`easylog.Close()` returns `ErrInitializing` if setup is still in progress; it does
not wait for or cancel setup. Wait for `Init` or `InitWithOutputs` to return before
closing the package runtime.
The runnable examples report both errors during deferred cleanup.
Terminal `Sync`/`Close` are no-ops: flush and close caller-owned writers yourself.

## Design and Contracts

### Encoding

Each enabled call builds a complete `slog.Record` and is encoded once by a standard
`slog.JSONHandler`. EasyLog retains fields prepared by `With` and combines them
with the call's fields using the nesting from `WithGroup`. Bound values are resolved,
replaced, and snapshotted at binding; direct fields and enrichers are prepared per
call, never once per destination. Duplicate keys and attribute order are preserved.

Preparation resolves `LogValuer` values and runs custom replacement before output.
Time-valued attributes become formatted strings; arbitrary values are snapshotted
using JSON encoding (or an error's string representation). JSON strings become
string attributes; other encoded values become owned `json.RawMessage` attributes.
Custom marshalers therefore run once, not separately for JSON and text. Treat these
prepared values as immutable.

The handler's capture writer copies the completed JSON line without delivering it.
After encoding returns, the handler passes the prepared `slog.Record` and JSON bytes
together to the runtime. Capture is serialized across a runtime's derived handlers,
and its lock is released before output delivery. Optional memory retains JSON only;
all outputs receive the same complete record and owned JSON bytes. Built-in text
output formats the record directly without decoding the JSON or evaluating user
values again. It reuses a buffer under the terminal's write lock. Logging is
synchronous and output writes are serialized. Preparation callbacks may run
concurrently; synchronize their mutable state.

Top-level user keys/groups `time`, `level`, `msg`, and `source` are reserved and
silently filtered, including replacement collisions and inlined unnamed groups.
User fields with reserved root keys are omitted before `ReplaceAttr`, without
resolving their values. For example, `logger.Info("hello", "msg", "override")` behaves like
`logger.Info("hello")`.
These names are allowed inside named groups. Built-in metadata is protected from
replacement. Record severity controls filtering and display; file routing reads
the corresponding JSON level.

### Custom Outputs

```go
type Output interface {
	WriteRecord(record slog.Record, jsonContent []byte) error
	Sync() error
	Close() error
}
```

The record is complete: it contains enriched, filtered, resolved and replaced
custom attributes, including bound fields and group nesting. `Time`, `Level`, and
`Message` contain the protected metadata. When `AddSource` is enabled, a prepared
`source` group holds its function, file, and line; formatters must not infer source
visibility from `PC` alone. A plain `slog.JSONHandler` with default options can
encode this record without further binding, replacement, or source generation.
Treat attributes, nested group slices, and raw JSON values as read-only; call
`record.Clone()` before adding attributes and deep-copy nested data before changing
it. Outputs may retain the record and JSON bytes without rerunning callbacks.

`terminal.TextFormatter.FormatRecord(record)` formats a prepared record directly.
It uses zero elapsed time and no automatic colors.

Outputs receive a complete JSON object with one trailing newline. They may retain
the slice but must not modify its backing array, including spare capacity; copy
before modifying. File and raw JSON outputs preserve bytes without adding a newline.
Underlying writers must obey `io.Writer` rules.

The runtime copies the output slice, skips nil entries, and manages `Sync`/`Close`.
Typed-nil outputs are unsupported. Every output is attempted in order; errors are
joined with indexes. Ordinary slog calls do not return handler errors. For explicit
error handling, use `Handler.Enabled` and `Handler.Handle` with a `slog.Record`, or
handle errors inside your output.

Output methods must not reenter logging, `Sync`, or `Close` on the same runtime:
this can deadlock. Coordinate ownership when sharing outputs across runtimes.

### File Details

- Directory ownership and access controls are the application's responsibility.
- Final JSON `level` chooses `debug_`, `info_`, `warn_`, or `err_`. Standard slog
  strings and offsets such as `INFO+2` are bucketed by severity. Missing, removed,
  renamed, unknown, or non-string levels route to INFO.
- Nonempty input must be a valid JSON object. Empty input is a no-op after the
  closed check. Restarts resume usable current-day segments.
- Sizes count exact input bytes, including newlines. Records are never split;
  an oversized record can exceed the threshold in an empty segment.
- Rotation attempts to sync the old segment but ignores sync failures. Explicit
  `Sync` reports active-segment errors, not earlier ignored failures.
- Resume and previous-segment close errors are returned even if fallback or rotation
	writes the current record successfully. Such an error does not imply the record
	was lost; blindly retrying may duplicate it.
- Cleanup runs on segment opening/rotation, not at startup or in the background.
	Failed deletion ends that attempt; later openings retry. Cleanup errors are ignored,
	so disk usage is not strictly bounded.
- Failed segment creation drops that file record without writing to the old segment
  or replaying later. Later calls retry creation; other outputs are still attempted.
- Short writes continue with the remainder. Partial-write errors are not replayed;
  actual bytes written count toward segment size.

### Text Details

Runtime text output reads the prepared record's message, time, level, and attributes
directly. Fields become `key=value`, with groups, objects, and arrays kept as JSON.
Control characters are escaped; DEBUG/ERROR prefixes shorten to DEBU/ERRO.

The record's level colors the prefix and field names: DEBUG gray, INFO cyan, WARN
yellow, ERROR red. Other slog levels are uncolored. Messages, timestamps, and
values keep the default color. Elapsed time is independent of the record's time;
a Go timestamp layout does not synthesize a zero timestamp. The reusable terminal
buffer retains its capacity after a large line.

### Lifecycle Details

Close disables new calls, waits for active output I/O, and closes every output.
Active preparation and encoding are not awaited. Under the output lock, calls check
for closure before retaining records or writing outputs. Once the first Close
finishes, no more records can enter memory; a captured consumer remains drainable.
Retention precedes that record's output I/O, including when destinations fail,
but waits behind earlier output I/O.

Duplicate closes return nil immediately; only the first waits and reports errors.
A blocked output can block shutdown indefinitely. Runtime `Sync` returns `ErrClosed`
once shutdown starts. Package `Close` restores previous slog and standard log
settings only if EasyLog is still the default. Coordinate global reconfiguration;
initialization returns `ErrAlreadyInitialized` while initializing, installed, or
closing and leaves rejected outputs untouched. Failed initialization releases its
reservation so setup can be retried.

Package state is not locked while initialization creates outputs or accesses Go's
global logger. Until the initialized runtime is published, `easylog.Consumer()`
returns nil and `easylog.Sync()` is a no-op. A concurrent `easylog.Close()` returns
`ErrInitializing`. Initialize before starting logging workers when possible, and
coordinate any external `slog.SetDefault` or standard-log reconfiguration.

## Development and Benchmarks

```sh
go build ./...
go vet ./...
go run -race ./main
```

There are no committed test or benchmark source files. The commands above compile,
check, and exercise the examples; they are not a comprehensive test suite.
Benchmark comparisons need a separate harness with matching payloads, output
settings, and allocation measurements.