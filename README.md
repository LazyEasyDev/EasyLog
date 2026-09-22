# EasyLog

Readable terminal logs, rotating JSON files, and optional in-memory retention,
built on Go's standard `log/slog`. Use ordinary slog calls with one or more outputs.

**Go 1.25+** | **[MIT license](LICENSE)**

[Quick start](#quick-start) | [Configuration](#configuration) |
[Logging](#logging) | [Outputs](#outputs) | [Memory](#memory) |
[Shutdown](#shutdown) | [Safety](#concurrency-and-callbacks)

## Install

```sh
go get github.com/LazyEasyDev/EasyLog
```

## Quick Start

Initialize the global logger once, then use `slog` throughout your application:

```go
package main

import (
	"fmt"
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func main() {
	if err := easylog.Init(easylog.InitOptions{
		Runtime:  easylog.Options{Level: slog.LevelInfo},
		Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
	}); err != nil {
		panic(err)
	}
	defer func() {
		if err := easylog.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	slog.Info("application started", "address", ":8080")
	slog.With("service", "checkout").Warn("request retrying", "attempt", 2)
}
```

```text
INFO[0000] application started address=:8080
WARN[0000] request retrying service=checkout attempt=2
```

`Init` installs `slog.Default()` and routes standard `log.Print` calls through
EasyLog too. This example writes only to stdout; it creates no files or memory store.
INFO logs are enabled by default, even when `Level` is omitted.

### Choose Your Setup

| Entry point | Best for | Shutdown |
| --- | --- | --- |
| `Init(InitOptions)` | Global logger with built-in outputs | `easylog.Close()` |
| `New(Options, []Output)` | Independent logger passed to your components | `runtime.Close()` |
| `InitWithOutputs(Options, []Output)` | Global logger with an explicit output list | `easylog.Close()` |

Use `Sync()` before `Close()` when you need to explicitly sync every output.
See [shutdown](#shutdown) for output ownership and error handling.

<details>
<summary><strong>Complete example: independent JSON logger</strong></summary>

```go
package main

import (
	"fmt"
	"log/slog"
	"os"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func main() {
	runtime := easylog.New(easylog.Options{Level: slog.LevelInfo}, []easylog.Output{
		terminal.NewJSON(os.Stdout),
	})
	defer func() {
		if err := runtime.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	logger := runtime.Logger().With("service", "orders")
	logger.Info("order received", "order_id", "order-42")
}
```

`New` leaves the global logger unchanged. `runtime.Logger()` returns a standard
`*slog.Logger`; `runtime.Handler()` exposes its handler. JSON output is NDJSON:
one JSON object per line, including `time`, `level`, `msg`, and your fields.

JSON record timestamps and time-valued slog attributes use UTC with nine fractional
digits, such as `2026-09-22T06:08:57.285866001Z` (30 characters for four-digit years).
If the nanosecond value ends in zero, it is advanced by one nanosecond so the
standard JSON handler retains all nine digits without a formatting hook. Other
timestamps keep their original instant. Zero record timestamps remain omitted.
If the UTC year is outside `0000` through `9999`, normalization returns zero time
without the one-nanosecond adjustment. The record's `time` field is then omitted;
custom time attributes use `0001-01-01T00:00:00Z` instead. This invalid-time fallback
is an exception to the fixed-width format.
Strings and times nested inside arbitrary JSON values are not rewritten.

</details>

## Configuration

Pass `Options` to `New` or `InitWithOutputs`. For `Init`, use `InitOptions.Runtime`.

| Option | Default | Purpose |
| --- | --- | --- |
| `Level` | `slog.LevelInfo` | Fixed threshold or a dynamic `*slog.LevelVar` |
| `AddSource` | `false` | Include the logging call's function, file, and line when available |
| `ReplaceAttr` | `nil` | Transform custom fields; built-in metadata is protected |
| `Enrichers` | `nil` | Extract fields from each enabled call's context |
| `MemoryMaxBytes` | `0` | Enable memory retention with a positive byte limit |

When `Level` is unset or a nil `*slog.LevelVar`, INFO and higher levels are emitted.
Set `Level: slog.LevelDebug` to include DEBUG, or `Level: slog.LevelWarn` to emit
only WARN and higher levels. Standard `log.Print` calls normally use INFO, so they
are included by default. Change the bridge level with `slog.SetLogLoggerLevel`.

> [!NOTE]
> Outputs and memory are opt-in. `InitOptions{}` and `New(Options{}, nil)` configure
> neither. A nil `InitOptions.File` or `InitOptions.Terminal` disables that output.

The recipes below are function-body snippets. Import the packages they use, and
reuse the independent example's `runtime` where one is not created. Short snippets
defer `Close` for brevity; use the error handling shown in [shutdown](#shutdown).
A minimal runnable application is in [main/main.go](main/main.go).

## Logging

### Fields and Groups

```go
logger := runtime.Logger()
logger.Info("order received", "order_id", "order-42", "items", 3)

service := logger.With("service", "checkout")
service.Info("ready", "address", ":8080")

payment := service.WithGroup("payment").With("provider", "sandbox")
payment.Info("authorized", "amount", 42.50)

logger.LogAttrs(context.Background(), slog.LevelInfo, "request completed",
	slog.Group("http", slog.String("method", "POST"), slog.Int("status", 201)),
)
```

Bind stable fields with `With(...)`; pass changing fields with each call.
Bound values are resolved and snapshotted once, so later mutations do not change
that logger's fields. Their JSON encoding is cached with source both on and off.
Structs, maps, and other JSON-compatible values can be used as field values.

### Dynamic Levels

```go
var level slog.LevelVar
runtime := easylog.New(easylog.Options{Level: &level}, []easylog.Output{
	terminal.NewJSON(os.Stdout),
})
defer runtime.Close()

runtime.Logger().Debug("hidden at the LevelVar's initial INFO level")
level.Set(slog.LevelDebug)
runtime.Logger().Debug("debug enabled")
```

A supplied zero-value `slog.LevelVar` starts at INFO, as defined by Go; EasyLog
does not overwrite it. For a fixed threshold, set `Level: slog.LevelDebug` directly.

### Source and Redaction

```go
runtime := easylog.New(easylog.Options{
	Level:     slog.LevelInfo,
	AddSource: true,
	ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "token" {
			return slog.String("token", "[REDACTED]")
		}
		return attr
	},
}, []easylog.Output{terminal.NewJSON(os.Stdout)})
defer runtime.Close()

runtime.Logger().Info("authorized", "token", "demo-token")
```

`ReplaceAttr` can rename, transform, or remove custom fields. Return `slog.Attr{}`
to remove a field. It runs once at binding for `With` fields and per call for new
fields, including children of slog groups. It does not inspect fields inside
arbitrary maps or structs; redact those values before logging.

**Top-level `time`, `level`, `msg`, and `source` are reserved.** User fields or
groups with those names are omitted, including replacement collisions. Built-in
metadata never reaches `ReplaceAttr`. These names are allowed inside named groups.

### Request Context

Use `Enrichers` to attach request-scoped fields. This example also imports `context`
and `log/slog`:

```go
type requestIDKey struct{}

runtime := easylog.New(easylog.Options{
	Level: slog.LevelInfo,
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

Use `InfoContext` or `LogAttrs(ctx, ...)` to pass request context. Plain `Info(...)`
uses a background context. Enrichers are skipped for disabled log calls.

## Outputs

### Terminal Text

Import `github.com/LazyEasyDev/EasyLog/terminal`. `terminal.New(writer, nil)` uses
elapsed timestamps, visible levels, and automatic colors. Start with the default
formatter when changing only a few settings:

```go
formatter := terminal.DefaultTextFormatter()
formatter.TimestampFormat = "15:04:05"
formatter.DisableColors = true

runtime := easylog.New(easylog.Options{Level: slog.LevelInfo}, []easylog.Output{
	terminal.New(os.Stdout, &formatter),
})
defer runtime.Close()
runtime.Logger().Info("ready", "service", "api")
```

| Setting | Effect |
| --- | --- |
| `TimestampFormat: ""` | Elapsed seconds since output creation |
| `TimestampFormat: "15:04:05"` | Record time formatted with a Go time layout |
| `DisableTimestamp: true` | Hide timestamps |
| `ShowLevel: true` | Show the level; does not change filtering |
| `DisableColors: true` | Disable all colors, even forced colors |
| `ForceColors: true` | Force ANSI colors without terminal detection |

Automatic colors respect `NO_COLOR`, `TERM=dumb`, and terminal capabilities.
Formatters are copied; a zero-value formatter hides the level. The level and field
names are colored, not the message or values. Groups and objects remain JSON;
control characters are escaped. DEBUG and ERROR display as DEBU and ERRO.

Detection runs once when the output is constructed and only for a direct
`*os.File`. Unix detection checks whether that file is a TTY and excludes the
exact monochrome `TERM` names `vt100`, `vt102`, and `vt220`, as well as `dumb`.
This is a small heuristic, not a terminfo query.

On Windows, EasyLog attempts to enable processed output and virtual-terminal
processing when automatic or forced colors are requested, preserving other
console flags. Automatic colors fall back to plain output if setup fails.
`DisableColors` prevents console changes; automatic `NO_COLOR`/`TERM=dumb`
opt-outs do too. Console mode changes are shared and are not restored by `Close`.

`ForceColors` overrides environment and terminal exclusions, and still emits
ANSI to pipes, custom writers, or unsupported consoles even if preparation fails.
`DisableColors` wins over everything. See [real-platform testing](integration/README.md)
for verification methods and coverage limits.

### JSON and Multiple Outputs

`terminal.NewJSON(writer)` sends raw NDJSON to any `io.Writer`, not just a terminal.
Combine outputs to send the same event to text and JSON destinations:

```go
if err := easylog.InitWithOutputs(easylog.Options{Level: slog.LevelInfo}, []easylog.Output{
	terminal.New(os.Stderr, nil),
	terminal.NewJSON(os.Stdout),
}); err != nil {
	panic(err)
}
defer easylog.Close()
slog.Info("inventory updated", "sku", "item-42", "quantity", 12)
```

Terminal outputs do not flush or close caller-owned writers. Direct terminal
constructors use stderr for a nil writer; `Init` instead disables terminal output
when `Terminal.Writer` is nil.

### Rotating Files

```go
directory, err := os.Getwd()
if err != nil {
	panic(err)
}
if err := easylog.Init(easylog.InitOptions{
	Runtime: easylog.Options{Level: slog.LevelInfo},
	File: &easylog.FileOptions{
		BaseDirectory:       directory,
		MaxSegmentBytes:     8 * 1024 * 1024,
		MaxSegmentsPerLevel: 7,
		Permissions:         0o600,
	},
	Terminal: &easylog.TerminalOptions{Writer: os.Stdout},
}); err != nil {
	panic(err)
}
defer func() {
	if err := easylog.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}()
slog.Info("written to terminal and file")
```

| File option | Default | Meaning |
| --- | --- | --- |
| `BaseDirectory` | Required | Absolute parent directory; files go into its `logs` subdirectory |
| `MaxSegmentBytes` | 8 MiB | Rotate before the next record exceeds the segment size |
| `MaxSegmentsPerLevel` | 7 | Retain up to this many segments per severity bucket, including active; best effort |
| `Permissions` | `0o644` | New-file permissions, before `umask`; must include owner read/write |

Files are named `debug_YYYYMMDD_N.jsonl`, `info_YYYYMMDD_N.jsonl`,
`warn_YYYYMMDD_N.jsonl`, and `err_YYYYMMDD_N.jsonl`. Rotation uses UTC dates and size.
Zero size/count settings select defaults; negative values are rejected.

> [!IMPORTANT]
> Use exactly one file output instance in one process per managed `logs` directory.
> There is no cross-instance or inter-process directory lock. Multiple goroutines
> may safely share one runtime, but separate file outputs can rotate or delete each
> other's files. Retention is not a hard disk quota.

Missing directories are created with `0755`, subject to `umask`. Existing directory
and file permissions are not changed. Modes such as `0o200` are rejected during
setup; use `0o600` or `0o644`. The process must retain read/write access after
`umask` and other access controls, because resuming a file reads its final byte.

For an independent runtime, import `github.com/LazyEasyDev/EasyLog/file`, create an
output with `file.New(file.Options{BaseDirectory: directory})`, check the returned
error, and pass it to `easylog.New`.

<details>
<summary><strong>File rotation, recovery, and error behavior</strong></summary>

- File `Close` syncs and closes every active segment, even if syncing fails. Both
  errors are returned with level and operation context.
- Restarts resume usable current-day segments. The JSON level selects the severity
  bucket; missing, unknown, or non-string levels fall back to INFO. Standard slog
  level offsets are supported.
- Bytes include the trailing newline. A record is never deliberately split across
  segments; one oversized record may exceed the limit in an empty segment.
- Cleanup runs on opening or rotation, not in the background. Deletion errors are
  ignored and stop that cleanup attempt; later openings retry.
- Rotation attempts to sync the old segment but currently ignores sync errors.
  Later `Sync` or `Close` cannot report those earlier failures.
- Resume or previous-segment close errors may be returned even when the new record
  was written. Do not blindly retry: you could duplicate the record.
- Creation failure drops that file record; other outputs are still attempted.
  Partial-write errors are not replayed. Short writes continue with the remainder.
- Direct `WriteRecord` input must be a JSON object with its newline already present.
  Empty input is a no-op unless the output is closed.

</details>

## Memory

Memory retention is **disabled by default**. Enable it with a positive byte limit:

```go
runtime := easylog.New(easylog.Options{
	Level:          slog.LevelInfo,
	MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
}, nil)
defer runtime.Close()

runtime.Logger().Info("job completed", "rows", 128)
page, err := runtime.Consumer().Take(100)
if err != nil {
	panic(err)
}
for _, line := range page {
	if _, err := os.Stdout.Write(line); err != nil {
		panic(err)
	}
}
```

This example retains JSON without live outputs. Memory can also run alongside
outputs. `DefaultMemoryMaxBytes` is 8 MiB, not an automatically enabled default.
`Consumer()` returns nil when retention is disabled; use `easylog.Consumer()` for
the global runtime.

| Method | Behavior |
| --- | --- |
| `Take(n)` with `n > 0` | Remove and return up to `n` oldest records |
| `Take(0)` | Drain the queue |
| `Take(n)` with `n < 0` | Return `ErrInvalidPageSize` without changing the queue |
| `Len()` | Retained record count |
| `Bytes()` | Retained JSON bytes, including newlines |

`Take` does not wait for new records; an empty queue returns `nil, nil`. Returned
lines are independent, mutable copies. Oldest records are evicted to fit; an
oversized record is skipped without evicting others. The limit covers JSON bytes,
not total heap usage. Spare ring-buffer slots remain allocated until fully drained.

Join lines with `bytes.Join(page, nil)` for NDJSON. For a JSON array use
`[]json.RawMessage`, since `[][]byte` marshals as base64 strings.

## Shutdown

**Stop and join logging workers first.** EasyLog does not stop your goroutines.
To explicitly sync every output, call `Sync`, then always call `Close`, even when
syncing fails:

```go
syncErr := runtime.Sync()
closeErr := runtime.Close()
if err := errors.Join(syncErr, closeErr); err != nil {
	fmt.Fprintln(os.Stderr, err)
}
```

This snippet also imports `errors` and `fmt`. Use `easylog.Sync()` and
`easylog.Close()` for global initialization.

| Output | What `Close()` does |
| --- | --- |
| Built-in file output | Attempts to sync and close every active file; returns both kinds of error |
| Terminal text / JSON | No-op; the caller must flush and close its writer |
| Custom output | Runs its own `Close`; the runtime does not separately call its `Sync` |

`Close` is idempotent. Concurrent/repeated **runtime** closes return nil immediately;
only the first waits and reports errors. A blocked output can block shutdown.
Syncing is a durability attempt, not a promise that logs survive every crash or
hardware failure.

## Concurrency and Callbacks

One runtime supports concurrent logging. Output delivery is synchronous and
serialized; a slow output delays other log calls. Preparation callbacks may run
concurrently, so protect any state they share.

> [!WARNING]
> **No reentry guard exists.** An output's `WriteRecord`, `Sync`, or `Close` must
> not log through the same runtime or call its `Sync` or `Close`. The outer call
> holds a mutex; reentry can deadlock instead of returning an error. This is a
> caller responsibility, not an automatically enforced check.

For custom outputs, return failures as errors instead of logging through that
runtime. Ordinary `slog.Info` and similar calls discard handler errors; use an
independent diagnostic logger or explicit handler error handling when needed.

`ReplaceAttr`, enrichers, `LogValuer.LogValue`, and custom JSON marshalers must not
call package/runtime `Sync` or `Close`. Avoid logging through the same runtime
from these callbacks: recursion can repeat indefinitely. Preparation is outside
EasyLog's capture/output locks, but a call from `log.Print` still holds the
standard logger's mutex. Recursive `log.Print`, or `easylog.Close()` restoring
that logger, can deadlock.

Create a separate diagnostic logger **once, outside callbacks**, using `log` and
`os`, then use it for callback/output diagnostics:

```go
diagnostic := log.New(os.Stderr, "easylog diagnostic: ", log.LstdFlags)
diagnostic.Print("output unavailable")
```

Its writer must not route back into EasyLog. To request shutdown from a callback,
signal code outside the logging call without blocking or waiting for completion,
then return. That code can stop workers and close the runtime.

## Custom Outputs

Implement `easylog.Output` and pass it to `New` or `InitWithOutputs`:

```go
type Output interface {
	WriteRecord(record slog.Record, jsonContent []byte) error
	Sync() error
	Close() error
}
```

| Input | Contract |
| --- | --- |
| `record` | Complete prepared record, including bound fields, groups, and resolved/replaced values |
| `jsonContent` | Owned JSON bytes ending in one newline; do not add another newline |
| Ownership | Both inputs may be retained, but are read-only, including nested values and spare byte capacity |

Clone the record before adding fields; deep-copy nested groups/raw JSON before
changing them. Copy JSON bytes before modifying them. Every output is attempted
after a returned error, and errors are joined with output indexes. Do not panic:
a panic propagates and can prevent later outputs from being called or closed.
Coordinate ownership when sharing outputs between runtimes. Plain nil entries are
skipped; typed-nil outputs are unsupported. Writers must follow `io.Writer` rules.

For explicit write errors, use `runtime.Handler().Enabled` and `.Handle` with a
`slog.Record`, or report errors independently inside the output. The
[no-reentry rule](#concurrency-and-callbacks) applies to all output methods.

## Reference

<details>
<summary><strong>Prepared records and encoding</strong></summary>

Each enabled call produces a complete `slog.Record` and one JSON line via the
standard `slog.JSONHandler`, even when only text output is configured. Custom
fields are prepared once, not once per destination. JSON capture is serialized
and its lock is released before outputs run. Text formats the record directly;
it does not parse JSON or reevaluate user values.

`With` prepares values and caches their JSON at binding. Per-call attributes and
enricher fields are prepared for each enabled call. Duplicate custom keys and
attribute order are preserved. Reserved root fields are dropped before resolution
or replacement; named groups may contain those same keys.

Time-valued attributes become formatted strings. Arbitrary values are snapshotted
using JSON encoding, with slog-style error strings where encoding fails. JSON
string results become string attributes; other JSON values become owned
`json.RawMessage` attributes. Treat prepared values as immutable.

When source is enabled and available, the output record has an explicit root
`source` group. `PC` alone does not imply it should be displayed. Encoding that
complete record with a default `slog.JSONHandler` preserves its fields, but not
necessarily byte order: runtime JSON uses native slog source-before-`msg` order;
the prepared source attribute is emitted after `msg` when reencoded.

`terminal.TextFormatter.FormatRecord(record)` formats a prepared record with
zero elapsed time and no automatic color detection. Runtime terminal output
reuses its buffer; capacity after a large line is retained.

</details>

<details>
<summary><strong>Initialization and lifecycle details</strong></summary>

- Initialize before starting logging workers. Coordinate external `slog.SetDefault`
  and standard-log changes with `Init`/`Close`; global restoration is not atomic.
- `Init` and `InitWithOutputs` reject initializing, installed, or closing state
  with `ErrAlreadyInitialized`. Rejected `InitWithOutputs` calls leave outputs
  untouched. Failed setup releases its reservation so initialization can be retried.
- During setup, package `Consumer()` returns nil, `Sync()` is a no-op, and `Close()`
  returns `ErrInitializing` without waiting or cancelling initialization.
- Package `Close` restores earlier `slog`/`log` settings only while the exact logger
	installed by `Init` or `InitWithOutputs` remains default. If you install a different
	default, you own its restoration, including EasyLog children and custom wrappers.
	EasyLog still closes its runtime; children or wrappers forwarding to that runtime
	can no longer write through it after shutdown.
- Runtime closure rejects new records and waits for output I/O, but does not wait
  for ongoing preparation/encoding. No records enter memory after shutdown
  completes; an already captured memory consumer remains drainable.
- Memory retention happens before output delivery, including when an output
  returns an error. It waits behind earlier output I/O.
- Custom output panics are not recovered. Package shutdown state is reset during
  unwinding, so initialization is possible after the caller recovers, but remaining
  outputs may not have been closed.

</details>

## Examples

From a checkout, run the minimal example:

```sh
go run ./main
```

It writes INFO, WARN, DEBUG, and ERROR records to stdout **and creates a `logs`
subdirectory in its working directory**. It demonstrates global initialization,
bound attributes, and rotating JSON files. There are no `-example`, `-log-dir`,
or `-help` flags; use the configuration recipes above for other setups. Run the
compiled example in a temporary directory when testing to avoid writing logs
into the checkout.

See [main/main.go](main/main.go) for full source and shutdown error handling.

## Development

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
go run ./integration/platformprobe
```

Formatter regression tests and a real-process platform probe are committed.
The probe uses temporary files, real Unix PTYs, or native Windows console cells;
see [integration testing](integration/README.md). It is not a comprehensive test
suite for every logging feature or terminal emulator. No benchmarks are included;
compare matching payloads, output formats, source/memory settings, and allocations.
JSON and text timings are not interchangeable.

The [platform runtime report](docs/platform-testing/README.md) includes real
Linux, FreeBSD, and Windows execution results, screenshots, and known color
detection limitations.

## License

EasyLog is available under the [MIT license](LICENSE).