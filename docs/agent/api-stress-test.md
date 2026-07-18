# API Stress Test

`api-stress-test/` must produce trustworthy results under high concurrency without becoming its own scheduling, memory, output, or shutdown bottleneck. Use `api-stress-test/go.mod` and `.github/workflows/api-stress-test-ci.yml` as the toolchain and CI sources of truth.

## First-Read Files

- `api-stress-test/cmd/root.go` - Cobra flags, normalization, validation, `StressTestOptions`, and typed exit-code mapping.
- `api-stress-test/cmd/lifecycle.go` - HTTP transport, warmup, scheduling/active contexts, graceful drain, signals, TTY detection, result translation, and schema-v2 assembly.
- `api-stress-test/cmd/report_file.go` and `cmd/report_rename_<os>.go` - atomic JSON report replacement, permission preservation, and platform-specific replacement semantics.
- `api-stress-test/internal/request/client.go` - body preparation, Content-Type precedence, HTTP timing, streaming response reads, decoded-byte counts, and structured request errors.
- `api-stress-test/internal/request/matcher.go` - immutable reusable body-expectation matcher.
- `api-stress-test/internal/request/ratelimiter.go` - global burst-one request-start pacing.
- `api-stress-test/internal/stats/collector.go` - structured samples, completion accounting, bounded total-latency/TTFB reservoirs, throughput, histograms, and errors.
- `api-stress-test/internal/ui/output.go` - text output and public JSON schema.
- `api-stress-test/internal/ui/progress.go` - live TTY-only progress rendering.

## CLI Surfaces

Important flags are defined in `cmd/root.go`:

- Target and method: `--url`, `--method`
- Load shape: `--requests`, `--concurrency`, `--timeout`, `--duration`, `--rate`, `--warmup`, `--shutdown-grace`
- Request data: `--headers`, `--data`, `--json-body`, `--json-file`, `--body`, `--file`, `--content-type`
- Transport behavior: `--insecure`, `--disable-keepalive`, `--disable-redirects`, `--proxy`
- Expectations: `--expect-status`, `--expect-body`
- Output: `--output`, `--output-file`

Preserve existing flag names, defaults, and the non-positive request/concurrency fallbacks unless a breaking change is explicitly approved. Omitted `--shutdown-grace` uses the request timeout; explicit `0s` cancels active work immediately after scheduling stops.

## Request And Scheduling Contracts

- Content-Type precedence is explicit `--content-type`, parsed `Content-Type` header, then body inference.
- `request.Result.TTFB` ends when response headers arrive. `Elapsed` ends after decoded body EOF/read failure. `ResponseSize` is the exact decoded byte count observed, including partial reads.
- Body expectations use one prepared immutable matcher per run and bounded per-request state. Continue reading after a match to preserve exact bytes, detect later errors, and enable connection reuse.
- The shared limiter spans warmup and measurement. It paces immediately before an unbuffered ready-worker dispatch, permits burst one, and never accumulates idle tokens.
- Warmup uses normal expectations but separate accounting. Continue when at least one warmup request succeeds; abort measurement when successes are zero.
- Scheduling and active-request contexts are distinct and preserve parent values. Duration expiry or the first interrupt stops new starts; active work drains for the effective grace. Grace expiry or a second signal cancels active work.
- Planned lifecycle cancellation is `Cancelled`, not `Failure`. Parent cancellation/SIGINT maps to exit 130, SIGTERM to 143, and signal exit takes precedence over measured failures.

## Metrics And Output Contracts

- `stats.Collector.Record` accepts a structured `stats.Sample` with explicit success/failure/cancelled outcome, total latency, TTFB, completion time, error kind, status, and decoded bytes.
- Total-latency and TTFB series have independent counts and bounded reservoirs. Throughput starts at the measured phase and retains leading empty seconds plus drain completions.
- JSON output is additive schema v2. Treat field names in `internal/ui/output.go` and `internal/ui/testdata/output_v2.golden.json` as a public contract; keep legacy `config`, `statistics`, timing, and rate fields compatible.
- Schema v2 records effective configuration, warmup summary, drain time, termination reason, completed/cancelled counts, TTFB metrics, and histogram sampling metadata. It records only whether an expected-body check is enabled, not the secret expectation text.
- Progress runs only for text output on a detected TTY. Lifecycle, progress, and final writes share a serialized writer and propagate the first write error.
- `--output-file` always writes schema-v2 JSON atomically in the destination directory. Preserve existing regular-file permissions and the prior report on create/write/sync/close/rename failure; replacing a symlink replaces the path entry rather than truncating its target.

## Verification

Run the controlled full suite and high-concurrency gate:

```bash
cd api-stress-test
env -u NO_COLOR go test ./...
env -u NO_COLOR go test -race ./...
```

Repeat lifecycle, pacing, matcher, and streaming semantics when those areas change:

```bash
go test ./cmd ./internal/request -run 'Test.*(Duration|Grace|Signal|Warmup|RateLimiter|Matcher|Truncated|ExactRequest)' -count=20
```

Run allocation gates and retain benchmark output for comparisons; tests own the stable allocation bounds, while nanosecond timings are informational:

```bash
go test ./... -run 'Test.*Allocations' -count=1
go test ./... -run '^$' -bench 'Benchmark(CollectorRecord|ResponseBodyStreaming|SchedulerIntegration)$' -benchmem
```

Use `env -u NO_COLOR go test ./internal/ui` for output tests. See `docs/agent/testing.md` for the full matrix and `.github/workflows/api-stress-test-ci.yml` for native cross-platform gates.
