# Benchmark Archive

This directory preserves raw measurements and summaries of the reports.
All Go test files, including benchmark sources, have been removed. The current
comparison below used a temporary external harness that was removed after the run.
Historical results do not measure current code. Filenames containing `current`
describe the implementation at measurement time only. The generic JSON-to-text
formatter has not been measured here.

## Current JSON Comparison

September 17, 2026: current JSON-only workspace implementation, Apple M4,
macOS `darwin/arm64`, Go 1.25.1, Zap v1.28.0. Six 500 ms samples per case,
`GOMAXPROCS=1`, no race instrumentation. The historical workloads and logger
configuration below were reused for EasyLog Info, slog Info, and Zap typed.
EasyLog used `terminal.NewJSON(io.Discard)` through its runtime; slog used
JSONHandler; Zap used a locked JSON sink. Source, sampling, memory retention,
enrichers, and custom replacement were disabled. All nine JSON-content,
timestamp, and NDJSON-framing parity checks passed before timing.

Median `ns/op` from [results-json-only-20260917-darwin-arm64.txt](results-json-only-20260917-darwin-arm64.txt):

| Implementation | Message | Fields10 | Context10 |
| --- | ---: | ---: | ---: |
| EasyLog Info | 582.80 | 1736.50 | 1633.00 |
| slog Info | 272.70 | 948.70 | 280.65 |
| Zap typed | 178.10 | 589.10 | 186.75 |

In Message/Fields10/Context10 order: EasyLog used 216 / 1312 / 1568 B/op
and 4 / 12 / 16 allocs/op; slog used 0 / 328 / 0 B/op and 0 / 4 / 0 allocs/op;
Zap used 0 / 704 / 0 B/op and 0 / 1 / 0 allocs/op. This measures serial JSON
CPU/allocation cost, not file I/O, text rendering, or concurrent scaling.

## Historical JSON Comparison

September 17, 2026: Apple M4, macOS `darwin/arm64`, Go 1.25.1, Zap v1.28.0;
six 500 ms samples per case, `GOMAXPROCS=1`, no race instrumentation.
INFO records went to one synchronized JSON destination backed by `io.Discard`,
with source, sampling, memory retention, enrichers, and custom replacement disabled.
`Message` had no fields; `Fields10` constructed ten fields per call; `Context10`
bound them before timing. This measures serial CPU/allocation cost, not disk I/O,
durability, terminal rendering, or concurrent scaling.

The last saved single-handler/full-NDJSON run still used prepared records.
Median `ns/op` from [results-json-single-handler-ndjson-darwin-arm64.txt](results-json-single-handler-ndjson-darwin-arm64.txt):

| Implementation | Message | Fields10 | Context10 |
| --- | ---: | ---: | ---: |
| EasyLog Info | 573.75 | 1956.0 | 1800.0 |
| slog Info | 275.55 | 967.8 | 281.65 |
| Zap typed | 177.9 | 598.8 | 187.15 |

EasyLog allocated 96 / 1736 / 1528 B/op and 1 / 8 / 7 allocs/op respectively.
These local results do not predict application performance or the current API's cost.

## Earlier Reports

The following summaries refer to separate historical runs, not a cumulative speedup.
Message/Fields10/Context10 values are listed in that order; times are `ns/op`.

| Experiment | Historical Finding | Saved Samples |
| --- | --- | --- |
| Original baseline | EasyLog Info 454.4 / 1521.0 / 1394.5; Zap Sugared 183.2 / 833.6 / 190.5 | [results-darwin-arm64.txt](results-darwin-arm64.txt) |
| Encoder reuse | EasyLog Info 451 / 1508 / 1386 to 400 / 1449 / 1329; four allocations and 208 B saved per call | [results-before-reuse-darwin-arm64.txt](results-before-reuse-darwin-arm64.txt), [results-after-reuse-darwin-arm64.txt](results-after-reuse-darwin-arm64.txt) |
| Direct JSON writes | EasyLog Info 401 / 1459 / 1335 to 367 / 1410 / 1304; one allocation saved per call | [results-before-direct-write-darwin-arm64.txt](results-before-direct-write-darwin-arm64.txt), [results-after-direct-write-darwin-arm64.txt](results-after-direct-write-darwin-arm64.txt) |
| Prepared-record text | Text 2427 / 8716.5 / 8635 to 834 / 3084 / 2904; JSON slowed by about 42-45% | [results-before-structured-text-darwin-arm64.txt](results-before-structured-text-darwin-arm64.txt), [results-after-structured-text-darwin-arm64.txt](results-after-structured-text-darwin-arm64.txt) |
| Removed metadata optimization | JSON Message 542.15 to 499.15; other workloads changed little | [results-current-json-darwin-arm64.txt](results-current-json-darwin-arm64.txt), [results-json-after-metadata-decision-darwin-arm64.txt](results-json-after-metadata-decision-darwin-arm64.txt) |

Message-stage diagnostics remain in [results-message-stages-darwin-arm64.txt](results-message-stages-darwin-arm64.txt)
and [results-message-after-metadata-decision-darwin-arm64.txt](results-message-after-metadata-decision-darwin-arm64.txt).
Stage costs are not additive predictions of optimization savings. Small differences
in unchanged control measurements also caution against attributing every timing change to code.

## Historical Newline Writes

The old newline-free input experiment compared copying JSON and appending a newline
for one OS write against two writes without copying. Current outputs instead receive
complete NDJSON lines. Saved medians from [results-framing-darwin-arm64.txt](results-framing-darwin-arm64.txt):

| Destination | JSON Bytes (Before Newline) | One Write With Copy | Two Writes Without Copy |
| --- | ---: | ---: | ---: |
| `/dev/null` | 128 / 1024 / 4096 | 398.6 / 409.3 / 441.3 | 781.7 / 783.1 / 784.5 |
| Regular file, append | 128 / 1024 / 4096 | 1094.0 / 1498.5 / 2755.0 | 2086.0 / 2499.5 / 3781.5 |

Times are `ns/op`; both strategies measured zero allocations after warmup.
Two writes added about 0.34-0.38 microseconds on `/dev/null` and 1 microsecond on
regular files. No fsync, encoding, routing, or rotation was timed; this is a net
write-strategy comparison, not isolated syscall latency or a durability measurement.