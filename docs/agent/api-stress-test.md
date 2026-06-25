# API Stress Test

`api-stress-test/` is the active high-concurrency project area. The tool must produce accurate stress-test results at 1000+ workers without bottlenecking on its own internals.

## First-Read Files

- `api-stress-test/cmd/root.go` - Cobra flags, validation, `StressTestOptions`, HTTP transport setup, worker fan-out, duration mode, warmup, rate limiter integration, and result output.
- `api-stress-test/internal/request/client.go` - headers, form data, JSON/raw/file body preparation, request execution, response draining, expected status/body checks, response byte counts, and error normalization.
- `api-stress-test/internal/request/ratelimiter.go` - `--rate` pacing.
- `api-stress-test/internal/stats/collector.go` - concurrent aggregation, success/failure counts, status counts, top errors, reservoir sampling, percentiles, histograms, throughput, and response byte totals.
- `api-stress-test/internal/ui/output.go` - text output and JSON output schema.
- `api-stress-test/internal/ui/progress.go` - live progress rendering and terminal update behavior.

## CLI Surfaces

Important flags are defined in `cmd/root.go`:

- Target and method: `--url`, `--method`
- Load shape: `--requests`, `--concurrency`, `--timeout`, `--duration`, `--rate`, `--warmup`
- Request data: `--headers`, `--data`, `--json-body`, `--json-file`, `--body`, `--file`, `--content-type`
- Transport behavior: `--insecure`, `--disable-keepalive`, `--disable-redirects`, `--proxy`
- Expectations: `--expect-status`, `--expect-body`
- Output: `--output`, `--output-file`

Preserve existing flag names and defaults unless the user explicitly requests a breaking change.

## Data Flow

1. `cmd/root.go` validates flags, parses durations/rates/expectations, builds `StressTestOptions`, and configures the HTTP client/transport.
2. Workers execute requests through `internal/request.ExecuteRequest`.
3. `internal/request/client.go` prepares request bodies and records status, latency, response size, and normalized errors.
4. `internal/stats.Collector.Record` aggregates results concurrently.
5. `internal/ui` renders text/JSON output and progress.

## Performance Risks

- Collector lock contention and latency storage can dominate throughput at high concurrency.
- Percentile and histogram logic must stay accurate when sampling or aggregation changes.
- Progress rendering must not serialize hot paths or print too often.
- Response draining and body close behavior affect connection reuse and worker throughput.
- Duration mode, cancellation, warmup, and rate limiting can skew request accounting if coordination changes.
- JSON/file body preparation should not be repeated per request unless that behavior is intentional.

## Verification

Run unit tests for any `api-stress-test` change:

```bash
cd api-stress-test && rtk go test ./...
```

For stats or contention-sensitive changes, add the benchmark:

```bash
cd api-stress-test && rtk go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem
```

Use package-specific tests for narrow changes:

```bash
cd api-stress-test && rtk go test ./internal/request
cd api-stress-test && rtk go test ./internal/ui
cd api-stress-test && rtk go test ./cmd
```

## Output Contracts

- Text output is user-facing terminal UI.
- JSON output is generated in `internal/ui/output.go`; treat struct field names as a public output contract.
- `--output-file` writes JSON results even when terminal output uses another format.

When changing output, update tests in `api-stress-test/internal/ui/` and keep examples in `README.md` aligned if user-facing behavior changes.
