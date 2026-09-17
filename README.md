# EasyLog

EasyLog is a synchronous [`log/slog`](https://pkg.go.dev/log/slog) backend that encodes
each enabled call once as JSON for terminal, rotating-file, custom outputs, and optional bounded memory.

## Quick Start

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
}
```

`Init` installs `slog.Default` and redirects standard `log`; this example writes text to stdout.
Nil `File`, `Terminal`, or `Terminal.Writer` disables that output; `InitOptions{}` enables no outputs or memory.

For an independent runtime, import `github.com/LazyEasyDev/EasyLog/terminal`:

```go
runtime := easylog.New(easylog.Options{
	Level: slog.LevelDebug, MemoryMaxBytes: easylog.DefaultMemoryMaxBytes,
}, []easylog.Output{terminal.NewJSON(os.Stdout)})
defer runtime.Close()
runtime.Logger().With("service", "api").Info("ready")
```

`New` leaves the default logger alone; `InitWithOutputs(options, outputs)` installs a package runtime.
Both copy the output slice, skip nil entries, and manage `Sync`/`Close`; typed-nil outputs are unsupported.

## Options and Encoding

`InitOptions.Runtime` accepts `Options`: `Level` (INFO by default; `slog.LevelVar` for changes),
`AddSource` for location, `ReplaceAttr` for JSON customization, and context `Enrichers`.
Positive `MemoryMaxBytes` enables retention; default off, `DefaultMemoryMaxBytes` is 8 MiB.

The standard `slog.JSONHandler` prepares stored `With`/`WithGroup` fields when they are bound
and reuses them on each enabled call. Bound `LogValuer` resolution, filtering, and `ReplaceAttr`
therefore run at binding time. Enrichers and fields passed directly to a log call run per call;
none of this work is repeated per output.
Top-level user keys/groups `time`, `level`, `msg`, and `source` are silently filtered,
including replacement collisions and inlined unnamed groups. Named nested keys and metadata replacement are allowed.
Level filtering uses the original slog severity; display and file routing use final JSON.

## Output Contract

```go
type Output interface {
	WriteRecord(jsonContent []byte) error
	Sync() error
	Close() error
}
```

Only a JSON object including the encoder's final newline is supplied, with no record or attribute reader.
The runtime copies it before recycling the buffer. Outputs may retain it but must not modify its
backing array (including spare capacity), shared with outputs and memory; copy before modifying.
File and `terminal.NewJSON` preserve exact bytes without adding a newline; writers obey `io.Writer` rules.
Writes attempt all outputs in order, serialized. Errors are joined with indexes; ordinary slog calls do not return them.

## Terminal and Standalone Formatting

`terminal.New(writer, nil)` uses elapsed seconds, `ShowLevel: true`, and automatic
colors: `INFO[0000] application started address=:8080`. Supplied formatters are copied;
their zero value hides the level. `terminal.NewJSON(writer)` writes raw NDJSON. Both use stderr for nil writers.

- `TimestampFormat: ""` shows elapsed seconds since output creation, independently of JSON time. A Go layout such as `"15:04:05"` formats RFC3339 JSON `time`; missing times are not synthesized in this mode.
- `DisableTimestamp` hides the timestamp and canonical `time`; `ShowLevel` controls only the displayed level, not filtering.
- `DisableColors` wins over `ForceColors`. Automatic colors respect `NO_COLOR`, `TERM=dumb`, and terminal detection; forcing colors requires an ANSI-capable destination.
- Final JSON `level` selects color for the prefix and field names: DEBUG/DEBU gray, INFO cyan, WARN/WARNING yellow, ERROR/ERRO red. Unknown or missing levels are uncolored. Messages, timestamps, and values keep the default color.
- Canonical string `msg` is displayed directly; other fields use `key=value`, with nested objects/arrays kept as JSON. Renamed metadata is ordinary data. DEBUG/ERROR prefixes shorten to DEBU/ERRO; control characters are escaped.

`TextFormatter.Format(jsonContent []byte) ([]byte, error)` formats arbitrary JSON without
slog types or a runtime. This produces `WARN cache miss key=item:42` followed by a newline:

```go
formatter := terminal.TextFormatter{
	DisableColors: true, DisableTimestamp: true, ShowLevel: true,
}
line, err := formatter.Format([]byte(`{"level":"WARN","msg":"cache miss","key":"item:42"}`))
if err != nil {
	panic(err)
}
os.Stdout.Write(line)
```

## Memory Retention

`runtime.Consumer()` or `easylog.Consumer()` is nil when retention is disabled.
`Take(pageSize int) ([][]byte, error)` is nonblocking and destructive: positive removes
up to that many oldest records, zero drains all, negative returns `ErrInvalidPageSize`
unchanged, and empty returns `nil, nil`. Returned lines are independent mutable copies.
`Len()` and `Bytes()` reflect removal; bytes and limits include newlines, not heap usage.
Oldest records are evicted to fit; oversized records are skipped without evicting others.
Retention precedes output I/O regardless of success; concurrent memory/output order may differ.
Join batches for NDJSON; use `[]json.RawMessage` for JSON arrays (`[][]byte` marshals as base64 strings).

## File Output

Enable with `File: &easylog.FileOptions{Directory: absolutePath}`; files go under `<Directory>/logs`.
UTC date/sequence names look like `info_20260917_0.jsonl`; restarts resume usable current-day segments.
Defaults: `MaxSegmentBytes` 8 MiB, `MaxSegments` seven per level (including active), `Permissions` 0644.
The **final JSON `level`**, not the original record, selects `debug_`, `info_`, `warn_`, or `err_`.
Standard slog strings, including offsets such as `INFO+2`, are bucketed by numeric severity.
Missing, removed, renamed, unknown, or non-string levels default to INFO. Nonempty input must be
a valid JSON object; invalid JSON/nonobjects are rejected. Empty input is a no-op after the closed check.

- Date changes and size thresholds rotate segments. Size counts exact input bytes, including newlines; records are never split, so an oversized record can exceed the threshold in an empty segment.
- Rotation attempts to sync the old segment but ignores sync failures. Explicit `Sync` reports active-segment errors, not ignored earlier failures.
- Retention is best effort: cleanup runs on segment opening/rotation, not at startup or in the background. Failed deletion stops that attempt; later openings retry. Successful writes do not report cleanup failures, so disk usage is not strictly bounded.
- Failed segment creation drops that file record and returns an error, without appending to the old segment or replaying later. Later calls retry creation; other outputs and memory are unaffected.
- Short writes continue with the remaining bytes; partial-write errors return without replay, and actual bytes written count toward size. Use only one output/process per managed directory.

## Lifecycle

Stop/join producers, `Sync` for durability, then `Close` and handle errors. **Close neither
calls Sync nor stops producers.** It disables new calls, waits for active output I/O,
and closes every output. Encoding/enrichers are not awaited and may still append to memory;
captured consumers or an independent runtime's consumer remain drainable after close.
Duplicate closes return nil immediately; only the first waits and reports errors. Blocked output can block shutdown indefinitely.
Output methods must not reenter logging, `Sync`, or `Close` on the same runtime: this can deadlock.
Terminal `Sync`/`Close` are no-ops; flush and close caller-owned writers yourself.
Package `Close` restores the previous slog logger and standard log writer/flags only
if EasyLog is still the default. Coordinate global reconfiguration and shared outputs.
Initialization returns `ErrAlreadyInitialized` while installed/closing, leaving rejected outputs untouched.
Runtime `Sync` returns `ErrClosed` once shutdown starts.

## Development

All Go test files, including benchmark sources, have been removed. Development checks:

```sh
go build ./...
go vet ./...
```

See [benchmarks/README.md](benchmarks/README.md) for archived results, not current-code timings or a runnable suite.