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

The only benchmark currently present is:

- `api-stress-test/internal/stats/collector_test.go`: `BenchmarkCollectorRecord`

The other tools currently have no test files:

- `case-converter/`
- `check-folder-size/`
- `common-module/`
- `find-content/`
- `replace-text/`

## Verification Matrix

| Change area | Minimum check |
| --- | --- |
| `api-stress-test/cmd/` | `cd api-stress-test && rtk go test ./cmd ./internal/...` |
| `api-stress-test/internal/request/` | `cd api-stress-test && rtk go test ./internal/request` |
| `api-stress-test/internal/stats/` | `cd api-stress-test && rtk go test ./internal/stats` |
| `api-stress-test` stats performance | `cd api-stress-test && rtk go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem` |
| `api-stress-test/internal/ui/` | `cd api-stress-test && rtk go test ./internal/ui` |
| `find-everything/internal/ui/` | `cd find-everything && rtk go test ./internal/ui` |
| Any module-wide change | `cd <tool-dir> && rtk go test ./...` |
| `common-module/utils/` | Test/build each importing consumer: `case-converter`, `check-folder-size`, `find-everything` |
| Docs-only change | `rtk git diff --check` plus path/link checks |

## Gaps To Consider

- Add focused tests when changing currently untested tools if behavior is non-trivial.
- `replace-text/main.go` deserves tests for binary detection, backup/restore behavior, temp-file rename failure paths, and recursive skip rules before larger changes.
- `find-content/searcher.go` deserves tests for regex, multiline, extension filtering, excluded dirs/files, and binary/text detection before search behavior changes.
- `check-folder-size/internal/scanner/scanner.go` deserves tests for depth, excludes, timeout cancellation, warning counts, and JSON output before traversal changes.
- `case-converter/main.go` deserves table tests for each supported output format before conversion logic changes.

## High-Concurrency Guidance

For `api-stress-test`, correctness matters more than just passing unit tests. For high-concurrency changes:

- Check request accounting under cancellation and duration mode.
- Check collector contention and allocation behavior.
- Check progress rendering does not dominate worker throughput.
- Keep benchmark comparisons reproducible and include `-benchmem`.
