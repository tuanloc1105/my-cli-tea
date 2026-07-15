# Testing

Use this file to choose the narrowest meaningful verification command.

## Current Test Coverage

Test files currently exist in:

- `api-stress-test/cmd/root_test.go`
- `api-stress-test/internal/request/client_test.go`
- `api-stress-test/internal/request/ratelimiter_test.go`
- `api-stress-test/internal/stats/collector_test.go`
- `api-stress-test/internal/ui/output_test.go`
- `api-stress-test/internal/ui/progress_test.go`
- `find-everything/internal/ui/display_test.go`
- `replace-text/cmd/root_test.go`
- `replace-text/internal/replacer/metadata_test.go`
- `replace-text/internal/replacer/metadata_unix_test.go`
- `replace-text/internal/replacer/processor_test.go`
- `replace-text/internal/replacer/processor_unix_test.go`
- `replace-text/internal/replacer/stream_test.go`
- `replace-text/internal/replacer/stream_fuzz_test.go`
- `replace-text/internal/replacer/types_test.go`

Benchmarks currently present are:

- `api-stress-test/internal/stats/collector_test.go`: `BenchmarkCollectorRecord`
- `replace-text/internal/replacer/stream_test.go`: `BenchmarkStreamReplace`

The current fuzz target is:

- `replace-text/internal/replacer/stream_fuzz_test.go`: `FuzzStreamReplace`

The other tools currently have no test files:

- `case-converter/`
- `check-folder-size/`
- `common-module/`
- `find-content/`

## Verification Matrix

| Change area | Minimum check |
| --- | --- |
| `api-stress-test/cmd/` | `cd api-stress-test && go test ./cmd ./internal/...` |
| `api-stress-test/internal/request/` | `cd api-stress-test && go test ./internal/request` |
| `api-stress-test/internal/stats/` | `cd api-stress-test && go test ./internal/stats` |
| `api-stress-test` stats performance | `cd api-stress-test && go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem` |
| `api-stress-test/internal/ui/` | `cd api-stress-test && go test ./internal/ui` |
| `find-everything/internal/ui/` | `cd find-everything && go test ./internal/ui` |
| `replace-text/cmd/` | `cd replace-text && go test ./cmd` |
| `replace-text/internal/replacer/` | `cd replace-text && go test ./internal/replacer` |
| `replace-text` concurrency or transaction behavior | `cd replace-text && go test -race ./...` |
| `replace-text` streaming replacement | `cd replace-text && go test ./internal/replacer -run '^$' -fuzz '^FuzzStreamReplace$' -fuzztime=10s` |
| Any module-wide change | `cd <tool-dir> && go test ./...` |
| `common-module/utils/` | Test/build each importing consumer: `case-converter`, `check-folder-size`, `find-everything` |
| Docs-only change | `git diff --check` plus path/link checks |

## Gaps To Consider

- Add focused tests when changing currently untested tools if behavior is non-trivial.
- `find-content/searcher.go` deserves tests for regex, multiline, extension filtering, excluded dirs/files, and binary/text detection before search behavior changes.
- `check-folder-size/internal/scanner/scanner.go` deserves tests for depth, excludes, timeout cancellation, warning counts, and JSON output before traversal changes.
- `case-converter/main.go` deserves table tests for each supported output format before conversion logic changes.

## High-Concurrency Guidance

For `api-stress-test`, correctness matters more than just passing unit tests. For high-concurrency changes:

- Check request accounting under cancellation and duration mode.
- Check collector contention and allocation behavior.
- Check progress rendering does not dominate worker throughput.
- Keep benchmark comparisons reproducible and include `-benchmem`.
