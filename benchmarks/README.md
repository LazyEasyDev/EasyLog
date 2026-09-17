# Logging Benchmarks

This suite compares EasyLog, Go's `slog.JSONHandler`, Zap, and Zap's
`SugaredLogger`. It follows the three workload shapes on
[Zap's performance page](https://github.com/uber-go/zap#performance), but uses
its own equivalent ten-field payload and shared JSON configuration. It does
not reproduce the exact upstream fixtures or published timings.

The suite is a separate Go module with a local replacement for EasyLog and a
pinned Zap dependency. It does not add Zap to EasyLog's production dependencies.
The root `go test ./...` does not include this nested module; test it explicitly.

## Method

The following settings describe `BenchmarkJSON`, the cross-library comparison.
`BenchmarkFormats` separately compares EasyLog JSON and text output using the
same three workloads and `Info` calls.
`BenchmarkFraming` isolates the final JSON-content/newline writes on real file
descriptors; see the newline write cost comparison below.

- `Message`: log a static INFO message without user fields.
- `Fields10`: construct and log ten fields on each call. Field construction is
  included in timing and allocations.
- `Context10`: attach those ten fields to a child logger once, before timing,
  then log the message repeatedly. Child construction is excluded.
- The fields are five strings, an int, an int64, a bool, a duration, and a float64.
  Typed and key/value builders represent the same data.
- `EasyLog` and `Slog` use `Info` and key/value arguments. Their `Attrs` variants
  use `LogAttrs` and typed attributes. `Zap` uses typed fields; `ZapSugar` uses
  `Infow` and key/value arguments.
- Every logger writes enabled INFO records to one `io.Discard` JSON destination,
  with RFC3339Nano timestamps, uppercase level names, and nanosecond durations.
  EasyLog uses `terminal.NewJSON`; its default text formatter is not measured.
- Caller output, sampling, memory retention, enrichers, and custom attribute
  replacement are disabled. EasyLog's normal reserved-key checks remain active.
- Each logger retains its normal synchronization. Zap's destination is locked,
  and the standard slog and EasyLog paths keep their existing locks.
- The serial `testing.B.Loop` body makes one logging closure call per operation
  for every variant. Logger construction and cleanup are outside timing.
- `TestEquivalentJSON` checks one complete NDJSON record, matching metadata and
  user values, allowing actual timestamps and JSON key order to differ.

The JSON comparison measures steady-state CPU/allocations, not disk throughput,
rotation/synchronization, terminal-text rendering, cold-start, memory-queue, or
concurrent-scaling benchmarks. Field-builder allocations are included, so these
results should not be interpreted as intrinsic API costs for every call style.

## Reproduce

Run these commands from the repository root with Go 1.25 or newer:

```sh
go -C benchmarks test -race ./...
go -C benchmarks vet ./...
GOTOOLCHAIN=local go -C benchmarks test -run '^$' -bench '^BenchmarkJSON$' \
  -benchmem -benchtime=500ms -count=6 -cpu=1
GOTOOLCHAIN=local go -C benchmarks test -run '^$' -bench '^BenchmarkFormats$' \
  -benchmem -benchtime=500ms -count=6 -cpu=1
GOTOOLCHAIN=local go -C benchmarks test -run '^$' -bench '^BenchmarkFraming$' \
  -benchmem -benchtime=500ms -count=6 -cpu=1
```

The original pre-reuse run saved stdout in
[results-darwin-arm64.txt](results-darwin-arm64.txt). Summarize those samples with
this pinned, Go-1.25-compatible `benchstat` version:

```sh
GOTOOLCHAIN=local go -C benchmarks run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20251208221838-04cf7a2dca90 \
  -row /workload -col /logger results-darwin-arm64.txt
```

Do not enable the race detector when measuring performance. Run measurements
without competing workloads, and rerun on the deployment hardware before making
capacity or latency decisions.

## Newline Write Cost

Measured on September 17, 2026: Apple M4, macOS 26.6.2 (`darwin/arm64`),
Go 1.25.1, six 500 ms samples per case, `GOMAXPROCS=1`, without the race detector.
Raw results are in [results-framing-darwin-arm64.txt](results-framing-darwin-arm64.txt).

- `OneWrite` uses the production `terminal.NewJSON` output: copy the content
  into its reusable buffer, append a newline, and write the complete line.
- `TwoWrites` is a benchmark-only alternative: write the content, then write
  a reusable one-byte newline. It does not copy the content. Both variants hold
  one output mutex across the complete record and handle short writes/errors.
- Both destinations use an unbuffered Go `*os.File`: `/dev/null`, or a temporary
  regular file opened with `O_APPEND`. `/dev/null` performs OS writes; it is not
  `io.Discard`. The regular-file benchmark does not call `Sync`/`fsync` and measures
  the OS accepting writes, not durable storage latency.
- Valid JSON payloads are generated before timing. Sizes below exclude the
  newline; both variants write the same total bytes. Encoding, routing, rotation,
  file creation, warmup, and cleanup are excluded. This is not the whole log call.
- Temporary files are capped at 32 MiB by truncating outside the timed portion.
  Final file sizes are checked, and the files are removed after each case.
  Separate tests verify exact contents, one versus two writer calls, and unchanged
  shared input bytes, including spare capacity.

Median time per record, in `ns/op` (rounded):

| Destination | JSON Bytes | One Write With Copy | Two Writes Without Copy | Extra ns/Record | Change |
| --- | ---: | ---: | ---: | ---: | ---: |
| `/dev/null` | 128 | 398.6 | 781.7 | 383.1 | +96.1% |
| `/dev/null` | 1024 | 409.3 | 783.1 | 373.8 | +91.3% |
| `/dev/null` | 4096 | 441.3 | 784.5 | 343.2 | +77.8% |
| Regular file, append | 128 | 1094.0 | 2086.0 | 992.0 | +90.7% |
| Regular file, append | 1024 | 1498.5 | 2499.5 | 1001.0 | +66.8% |
| Regular file, append | 4096 | 2755.0 | 3781.5 | 1026.5 | +37.3% |

Both variants measured **0 B/op and 0 allocs/op** after warmup. All timing
differences have `p=0.002`, with six samples per variant. The reported timing
ranges were within 1-3% except the one-write 1 KiB/4 KiB regular-file cases,
which were within 7%.

For these records, avoiding the copy did not offset the second OS write:
it added about 0.34-0.38 microseconds on `/dev/null` and about 1 microsecond on
the regular file. This delta includes Go descriptor locking and write machinery,
filesystem work, and the avoided copy; it is not an isolated syscall-entry cost.
Go's `internal/poll.FD.Write` calls `syscall.Write` synchronously and does not batch
the two calls. The kernel can still cache data and combine later physical disk I/O.

These are serial, cache-influenced local measurements, not guarantees for other
filesystems, hardware, buffered writers, contended writers, or interactive
terminal rendering. Production output code was not changed for this experiment.

Summarize the saved samples with:

```sh
GOTOOLCHAIN=local go -C benchmarks run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20251208221838-04cf7a2dca90 \
  -row /sink,/bytes -col /mode results-framing-darwin-arm64.txt
```

## Structured Output Comparison

These measurements predate the switch to newline-free `jsonContent`. Built-in
JSON outputs now copy that content into reusable output-owned buffers to add a
newline. That framing change has not been timed in the comparisons below.

The structured-output change introduced a prepared `slog.Record` and shared JSON
encoding. Terminal text renders the prepared attributes directly instead of copying
and parsing the complete JSON record. Attribute replacement and custom value
snapshotting happen once before fan-out, not independently for each output.

`BenchmarkFormats` compares `terminal.NewJSON(io.Discard)` with
`terminal.New(io.Discard, &terminal.TextFormatter{DisableColors: true, ShowLevel: true})`.
Text uses the default elapsed timestamp. Both modes still include the runtime's
one JSON encoding per log call; the text-only case does not skip JSON generation.
Memory, enrichers, and custom replacement are disabled. Construction is untimed,
and there is no actual terminal or disk I/O.

Before/after runs used Apple M4, macOS `darwin/arm64`, Go 1.25.1, six 500 ms
samples per case, and `GOMAXPROCS=1`. The workload and destination configuration
were unchanged. Raw samples are in
[results-before-structured-text-darwin-arm64.txt](results-before-structured-text-darwin-arm64.txt)
and [results-after-structured-text-darwin-arm64.txt](results-after-structured-text-darwin-arm64.txt).

Text is approximately 65-66% faster in these workloads. JSON-only calls regress
by approximately 42-45%: the new output contract prepares a retained structured
representation even when the destination only writes JSON. This is a text-path
improvement, not a universal logging speedup.

Median time per call (`ns/op`, lower is better):

| Format | Workload | Before | After | Measured Change |
| --- | --- | ---: | ---: | ---: |
| JSON | Message | 378.4 | 549.2 | +45.14% |
| Text | Message | 2427.0 | 834.0 | -65.64% |
| JSON | Fields10 | 1407.5 | 1993.5 | +41.63% |
| Text | Fields10 | 8716.5 | 3084.0 | -64.62% |
| JSON | Context10 | 1295.0 | 1834.5 | +41.66% |
| Text | Context10 | 8635.0 | 2904.0 | -66.37% |

Allocation measurements were consistent across samples:

| Format | Workload | B/op Before | B/op After | Allocs/op Before | Allocs/op After |
| --- | --- | ---: | ---: | ---: | ---: |
| JSON | Message | 96 | 96 | 1 | 1 |
| Text | Message | 2704 | 496 | 59 | 8 |
| JSON | Fields10 | 1192 | 1736 | 9 | 8 |
| Text | Fields10 | 7866 | 2640 | 219 | 18 |
| JSON | Context10 | 1448 | 1528 | 13 | 7 |
| Text | Context10 | 8130 | 2432 | 223 | 17 |

Some allocation counts decrease while allocated bytes or execution time
increase. Do not equate allocation count alone with speed. These serial local
measurements do not establish concurrent scaling or I/O throughput.

Compare both runs with:

```sh
GOTOOLCHAIN=local go -C benchmarks run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20251208221838-04cf7a2dca90 \
  -row /workload,/format \
  results-before-structured-text-darwin-arm64.txt results-after-structured-text-darwin-arm64.txt
```

## Direct JSON Write Comparison

These historical runs predate the prepared-record output interface. See the
structured output comparison above for the later JSON and text measurements.

After encoder reuse, a second change removes the record-to-output byte copy
from the built-in file and JSON-terminal paths. The record owns the completed
JSON line, including its newline, and an internal helper writes that line under
the existing output lock. The encoder-to-record ownership copy remains. At the
time, the memory snapshot's `JSON()` method copied and excluded the newline,
and `Size()` also excluded it. That custom snapshot type has since been removed.

The comparison used fresh before/after runs on the same Apple M4, Go 1.25.1,
Zap v1.28.0, six 500 ms samples per case, and `GOMAXPROCS=1`. Encoder reuse was
enabled in both runs. Workloads and benchmark configuration were unchanged.
Raw samples are in
[results-before-direct-write-darwin-arm64.txt](results-before-direct-write-darwin-arm64.txt)
and [results-after-direct-write-darwin-arm64.txt](results-after-direct-write-darwin-arm64.txt).

Median time per call (rounded to whole `ns/op`):

| API | Workload | Before | After | Measured Change |
| --- | --- | ---: | ---: | ---: |
| EasyLog Info | Message | 401 | 367 | -8.37% |
| EasyLog Info | Fields10 | 1459 | 1410 | -3.39% |
| EasyLog Info | Context10 | 1335 | 1304 | -2.28% |
| EasyLog LogAttrs | Message | 408 | 370 | -9.21% |
| EasyLog LogAttrs | Fields10 | 1498 | 1452 | -3.07% |
| EasyLog LogAttrs | Context10 | 1343 | 1319 | -1.79% |

Each EasyLog variant removes one heap allocation per log call:

| API | Workload | B/op Before | B/op After | Allocs/op Before | Allocs/op After |
| --- | --- | ---: | ---: | ---: | ---: |
| EasyLog Info | Message | 192 | 96 | 2 | 1 |
| EasyLog Info | Fields10 | 1448 | 1192 | 10 | 9 |
| EasyLog Info | Context10 | 1704 | 1448 | 14 | 13 |
| EasyLog LogAttrs | Message | 192 | 96 | 2 | 1 |
| EasyLog LogAttrs | Fields10 | 1864 | 1608 | 11 | 10 |
| EasyLog LogAttrs | Context10 | 1704 | 1448 | 14 | 13 |

In the after-run, plain slog Info took approximately 272, 924, and 281 ns/op
for the three workloads, while typed Zap took 179, 587, and 187 ns/op. The
allocation reduction is consistent across samples. Small timing differences
should be treated cautiously: unchanged plain slog Fields10 also improved by
about 3.7%, and the baseline Context10 LogAttrs samples included an outlier.

The measurements still use a single JSON destination with `io.Discard`. They do
not measure disk throughput or terminal text formatting, and removing this copy
does not cache bound `With` fields or change callbacks.

Compare both runs with:

```sh
GOTOOLCHAIN=local go -C benchmarks run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20251208221838-04cf7a2dca90 \
  -row /workload,/logger \
  results-before-direct-write-darwin-arm64.txt results-after-direct-write-darwin-arm64.txt
```

## Encoder Reuse Comparison

A fresh before/after comparison used the same Apple M4, Go 1.25.1, Zap v1.28.0,
six 500 ms samples per case, and `GOMAXPROCS=1`. The workload code and logger
configuration were unchanged. Raw samples are in
[results-before-reuse-darwin-arm64.txt](results-before-reuse-darwin-arm64.txt) and
[results-after-reuse-darwin-arm64.txt](results-after-reuse-darwin-arm64.txt).

The change pools each runtime's base `slog.JSONHandler` and scratch buffer, then
copies finished JSON into independently owned record bytes before recycling the
encoder. It does not cache `With` fields or change their callback timing. Scratch
capacity above 64 KiB is released when returning the encoder to the pool.

Median time per call (rounded to whole `ns/op`):

| API | Workload | Before | After | Measured Change |
| --- | --- | ---: | ---: | ---: |
| EasyLog Info | Message | 451 | 400 | -11.27% |
| EasyLog Info | Fields10 | 1508 | 1449 | -3.91% |
| EasyLog Info | Context10 | 1386 | 1329 | -4.08% |
| EasyLog LogAttrs | Message | 456 | 398 | -12.69% |
| EasyLog LogAttrs | Fields10 | 1550 | 1492 | -3.74% |
| EasyLog LogAttrs | Context10 | 1395 | 1341 | -3.87% |

The allocation reduction was four allocations and 208 allocated bytes per call
for each EasyLog case:

| API | Workload | B/op Before | B/op After | Allocs/op Before | Allocs/op After |
| --- | --- | ---: | ---: | ---: | ---: |
| EasyLog Info | Message | 400 | 192 | 6 | 2 |
| EasyLog Info | Fields10 | 1656 | 1448 | 14 | 10 |
| EasyLog Info | Context10 | 1912 | 1704 | 18 | 14 |
| EasyLog LogAttrs | Message | 400 | 192 | 6 | 2 |
| EasyLog LogAttrs | Fields10 | 2072 | 1864 | 15 | 11 |
| EasyLog LogAttrs | Context10 | 1912 | 1704 | 18 | 14 |

This is a modest timing improvement, not elimination of the Zap gap. In the
after-run, typed Zap took approximately 177, 588, and 185 ns/op for the three
workloads; Zap Sugared took 183, 822, and 190 ns/op. EasyLog still rebuilds derived
handlers and encodes bound fields per record, in addition to its record ownership
and output-coordination work.

Most control measurements were close to their baseline, but plain slog's
Fields10 case also improved by about 4% without any implementation change.
Do not attribute every small timing difference solely to pooling. The allocation
reduction is consistent across all samples; no concurrent-throughput or disk-I/O
performance claim is made here.

Compare both runs with:

```sh
GOTOOLCHAIN=local go -C benchmarks run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20251208221838-04cf7a2dca90 \
  -row /workload,/logger \
  results-before-reuse-darwin-arm64.txt results-after-reuse-darwin-arm64.txt
```

## Original Baseline (No Reuse)

- CPU/platform: Apple M4, macOS (`darwin/arm64`).
- Compiler: Go 1.25.1; Zap: v1.28.0.
- EasyLog: working tree based on `a840bc4`, with no production-code changes for
  this comparison.
- Six 500 ms samples per case, `GOMAXPROCS=1`, no race instrumentation.
- Values below are medians reported by `benchstat`. Lower is better.

### Time

Nanoseconds per operation (`ns/op`):

| Implementation | Message | Fields10 | Context10 |
| --- | ---: | ---: | ---: |
| EasyLog Info | 454.4 | 1521.0 | 1394.5 |
| EasyLog LogAttrs | 458.8 | 1563.0 | 1406.0 |
| slog Info | 271.0 | 962.0 | 279.1 |
| slog LogAttrs | 275.3 | 1003.5 | 280.6 |
| Zap typed | 178.0 | 597.5 | 185.4 |
| Zap Sugared | 183.2 | 833.6 | 190.5 |

### Allocated Bytes

Bytes allocated per operation (`B/op`), not retained heap size:

| Implementation | Message | Fields10 | Context10 |
| --- | ---: | ---: | ---: |
| EasyLog Info | 400 | 1656 | 1912 |
| EasyLog LogAttrs | 400 | 2072 | 1912 |
| slog Info | 0 | 328 | 0 |
| slog LogAttrs | 0 | 744 | 0 |
| Zap typed | 0 | 704 | 0 |
| Zap Sugared | 0 | 1408 | 0 |

### Allocations

Heap allocations per operation (`allocs/op`):

| Implementation | Message | Fields10 | Context10 |
| --- | ---: | ---: | ---: |
| EasyLog Info | 6 | 14 | 18 |
| EasyLog LogAttrs | 6 | 15 | 18 |
| slog Info | 0 | 4 | 0 |
| slog LogAttrs | 0 | 5 | 0 |
| Zap typed | 0 | 1 | 0 |
| Zap Sugared | 0 | 1 | 0 |

## Original Baseline Interpretation

Zap was faster in all three local workloads. Comparing key/value APIs,
EasyLog Info took approximately 2.5x, 1.8x, and 7.3x as long as Zap Sugared for
Message, Fields10, and Context10 respectively. The reported timing uncertainty
ranged up to 7%; small differences between Info and LogAttrs are not a general
performance conclusion.

The largest gap was with pre-bound context. The baseline EasyLog implementation
constructed a fresh JSON handler and replayed its stored `With` attributes on
each record, while Zap and the standard slog JSON handler reused pre-encoded
bound fields. EasyLog also performed its runtime filtering, record copying,
and serialized output fan-out.
The benchmarks measure that combined path; they do not isolate the cost of each
step. No encoder caching, pooling, or logging behavior was changed for that
original baseline run. Encoder reuse and direct-write results are recorded
separately above.

The plain slog baseline distinguishes the standard encoder's performance from
EasyLog's additional work. These results do not predict full application
performance or the cost of EasyLog features excluded from this comparison.