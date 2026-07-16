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
- `check-folder-size/cmd/root_test.go`
- `check-folder-size/internal/scanner/engine_test.go`
- `check-folder-size/internal/scanner/scanner_test.go`
- `check-folder-size/internal/scanner/scanner_unix_test.go`
- `check-folder-size/internal/scanner/scanner_windows_test.go`
- `check-folder-size/internal/scanner/types_test.go`
- `check-folder-size/internal/ui/printer_test.go`
- `find-content/cmd/root_test.go`
- `find-content/internal/searcher/matcher_test.go`
- `find-content/internal/searcher/reader_test.go`
- `find-content/internal/searcher/searcher_test.go`
- `find-content/internal/searcher/special_unix_test.go`
- `find-content/internal/searcher/hidden_darwin_test.go`
- `find-content/internal/searcher/hidden_windows_test.go`
- `find-everything/cmd/root_test.go`
- `find-everything/internal/finder/walker_test.go`
- `find-everything/internal/finder/walker_darwin_test.go`
- `find-everything/internal/finder/walker_unix_test.go`
- `find-everything/internal/finder/walker_windows_test.go`
- `find-everything/internal/types/types_test.go`
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
- `find-content/internal/searcher/matcher_test.go`: `BenchmarkMatcher`
- `find-content/internal/searcher/reader_test.go`: `BenchmarkReader`
- `find-content/internal/searcher/searcher_test.go`: `BenchmarkCoordinator`

The current fuzz target is:

- `replace-text/internal/replacer/stream_fuzz_test.go`: `FuzzStreamReplace`
- `find-content/internal/searcher/matcher_test.go`: `FuzzMatcher`

`common-module/` is the only module without test files. Verify it through `check-folder-size`, its importing consumer, when shared utilities change.

## Verification Matrix

| Change area | Minimum check |
| --- | --- |
| `api-stress-test/cmd/` | `cd api-stress-test && env -u NO_COLOR go test ./cmd ./internal/...` |
| `api-stress-test/internal/request/` | `cd api-stress-test && go test ./internal/request` |
| `api-stress-test/internal/stats/` | `cd api-stress-test && go test ./internal/stats` |
| `api-stress-test` stats performance | `cd api-stress-test && go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem` |
| `api-stress-test/internal/ui/` | `cd api-stress-test && env -u NO_COLOR go test ./internal/ui` |
| `check-folder-size/cmd/` | `cd check-folder-size && go test ./cmd` |
| `check-folder-size/internal/scanner/` | `cd check-folder-size && go test ./internal/scanner` |
| `check-folder-size` accounting/concurrency | `cd check-folder-size && go test -race ./...` |
| `check-folder-size` platform metadata | Run native scanner tests on the target OS; at minimum cross-build Darwin, Linux, and Windows with a toolchain satisfying `check-folder-size/go.mod` |
| `find-content/cmd/` | `cd find-content && go test ./cmd` |
| `find-content/internal/searcher/` | `cd find-content && go test ./internal/searcher` |
| `find-content` concurrency/order | `cd find-content && go test -race ./... && go test ./... -run 'Deterministic\|Ordering\|MaxResults\|Coordinator' -count=20` |
| `find-content` matcher fuzz | `cd find-content && go test ./... -run '^$' -fuzz '^FuzzMatcher$' -fuzztime=10s` |
| `find-content` cross-platform CI | `.gitea/workflows/find-content-ci.yml` runs native tests, vet, and builds on Ubuntu, macOS, and Windows; Linux also runs race and determinism gates |
| `find-everything/cmd/` | `cd find-everything && go test ./cmd` |
| `find-everything/internal/finder/` | `cd find-everything && go test ./internal/finder` |
| `find-everything` concurrency/cancellation | `cd find-everything && go test -race ./internal/finder -run 'Test.*(Limit|Queue|Cancel|Partial)' -count=20` |
| `find-everything` cross-platform policies | `.gitea/workflows/find-everything.yml` runs native hidden and symlink tests on Ubuntu, macOS, and Windows |
| `find-everything/internal/ui/` | `cd find-everything && go test ./internal/ui` |
| `replace-text/cmd/` | `cd replace-text && go test ./cmd` |
| `replace-text/internal/replacer/` | `cd replace-text && go test ./internal/replacer` |
| `replace-text` concurrency or transaction behavior | `cd replace-text && go test -race ./...` |
| `replace-text` streaming replacement | `cd replace-text && go test ./internal/replacer -run '^$' -fuzz '^FuzzStreamReplace$' -fuzztime=10s` |
| `replace-text` cross-platform CI | `.github/workflows/replace-text-ci.yml` runs test, vet, and build checks on GitHub-hosted Ubuntu, macOS, and Windows runners |
| Any module-wide change | `cd <tool-dir> && go test ./...` |
| `common-module/utils/` | Test/build the importing consumer: `check-folder-size` |
| Docs-only change | `git diff --check` plus path/link checks |

## Gaps To Consider

- Add focused tests for any newly introduced CLI behavior that is not covered by the command fixtures.
- Extend `find-content/internal/searcher/searcher_test.go` when changing traversal, ordering, cancellation, or result-cap semantics.
- Run `check-folder-size/internal/scanner/scanner_windows_test.go` on native Windows when changing allocation, file identity, reparse-point, or hidden-attribute handling; cross-build alone does not validate runtime filesystem semantics.
- Add direct `common-module/` tests if shared utilities gain behavior that cannot be characterized safely through consumers.

## Cobra Command Guidance

All five CLIs have command-package tests for flags, streams, errors, exit codes, and fresh invocation state. Follow `docs/agent/cli-conventions.md` and include two sequential invocations with different flags whenever command state changes.

`check-folder-size` scanner coverage includes allocated/logical accounting,
directory blocks, hidden entries, symlinks, special entries, sparse files,
global hardlink dedupe and deterministic attribution, depth/exclusion,
cancellation, warning-only partial results, metadata/read failures, overflow,
and non-nil empty results. Allocation assertions use filesystem invariants and
skip with a reason when the host does not expose the required capability.

`api-stress-test/internal/ui/TestColorWriterFORCE_COLOR` is sensitive to an inherited `NO_COLOR`. Use `env -u NO_COLOR go test ./...` for the controlled full-module result, and report a separate plain `go test ./...` run when diagnosing the ambient environment.

## High-Concurrency Guidance

For `api-stress-test`, correctness matters more than just passing unit tests. For high-concurrency changes:

- Check request accounting under cancellation and duration mode.
- Check collector contention and allocation behavior.
- Check progress rendering does not dominate worker throughput.
- Keep benchmark comparisons reproducible and include `-benchmem`.
