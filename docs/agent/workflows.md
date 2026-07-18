# Workflows

Run Go commands from an individual module unless the command explicitly loops over modules.

## GitHub Actions CI

GitHub Actions workflows verify `api-stress-test`, `check-folder-size`, `find-content`, `find-everything`, and `replace-text` on pushes, pull requests, and manual runs when their module or workflow changes. They run module tests, vet, and trimmed builds on hosted Linux, macOS, and Windows; use each module's `go.mod` and workflow as the toolchain source of truth.

`check-folder-size`, `find-content`, and `find-everything` also run the race detector on Linux. `find-content` repeats its determinism/result-cap tests on Linux, while `find-everything` runs native OS-specific hidden-entry and symlink policy tests on every platform.

`.github/workflows/api-stress-test-ci.yml` adds Linux race and repeated lifecycle/streaming semantics, stable allocation gates, and an uploaded collector/body/scheduler benchmark artifact. Benchmark timings are comparison data, not hosted-runner pass/fail thresholds.

`.github/workflows/common-module-ci.yml` runs the shared module checks and tests/builds `check-folder-size`, its importing consumer, on all three operating systems.

The public CI mirror is `https://github.com/tuanloc1105/my-cli-tea`. This checkout keeps Gitea as its fetch source and uses multiple `origin` push URLs so one push updates both Gitea and GitHub. That dual-push configuration lives in local Git config; other clones must configure their own GitHub push destination.

## Gitea Actions CI

`.gitea/workflows/find-everything.yml` verifies `find-everything` on native
`ubuntu-latest`, `macos-latest`, and `windows-latest` runners; use the module and workflow as the toolchain source of truth.
Every job runs module verification, tests, vet, and build; Linux also runs the
race detector. The native finder-policy step exercises the OS-specific hidden
attribute tests and symlink contract. Windows runners must allow symlink
creation through Developer Mode or `SeCreateSymbolicLinkPrivilege`; the test
fails with that prerequisite instead of silently skipping.

`.gitea/workflows/find-content-ci.yml` verifies `find-content` on Ubuntu, macOS, and Windows when the module or workflow changes. Every OS runs `go test ./...`, `go vet ./...`, and `go build -trimpath ./...`; Linux also runs the race detector and the repeated determinism/result-cap gate.

## Local Build

Build one module without installing:

```bash
cd <tool-dir> && go build -o <tool-name> .
```

Build all CLI modules locally without using the install-oriented `Makefile`:

```bash
bash -lc 'for d in api-stress-test check-folder-size find-content find-everything replace-text; do (cd "$d" && go build -trimpath -o "/tmp/$d" .); done'
```

The root `Makefile` uses `CGO_ENABLED=0` and platform-specific install paths. Prefer the local build loop when you only need compile verification.

## Makefile

Available targets:

- `make all`
- `make check-folder-size`
- `make find-content`
- `make find-everything`
- `make replace-text`
- `make api-stress-test`
- `make clean`

Install/move behavior:

- macOS: installs to `$(HOME)/dev-kit/tool`.
- Linux: installs to `/usr/local/bin` via `sudo mv`.
- Windows/MSYS: installs to `D:/dev-kit/tool`.

Use `make clean` after local builds if generated binaries are not needed. `.gitignore` ignores `*.exe` but not Unix binary names.

## Tests

Test one module:

```bash
cd <tool-dir> && go test ./...
```

Test all modules:

```bash
bash -lc 'for d in api-stress-test check-folder-size common-module find-content find-everything replace-text; do if [ "$d" = api-stress-test ]; then (cd "$d" && env -u NO_COLOR go test ./...); else (cd "$d" && go test ./...); fi; done'
```

Run plain `cd api-stress-test && go test ./...` separately when checking the inherited environment. With `NO_COLOR=1`, `internal/ui/TestColorWriterEnvironment/force_color` intentionally observes `NO_COLOR` precedence; the controlled command above removes that variable.

Run the `api-stress-test` allocation gates and benchmark suite:

```bash
cd api-stress-test
go test ./... -run 'Test.*Allocations' -count=1
go test ./... -run '^$' -bench 'Benchmark(CollectorRecord|ResponseBodyStreaming|SchedulerIntegration)$' -benchmem
```

## Vet, Format, Tidy

Vet all modules:

```bash
bash -lc 'for d in api-stress-test check-folder-size common-module find-content find-everything replace-text; do (cd "$d" && go vet ./...); done'
```

List files that need formatting:

```bash
bash -lc 'gofmt -l $(rg --files -g "*.go")'
```

Format only files you intentionally changed:

```bash
gofmt -w <changed-file.go>
```

Tidy all modules only when dependency metadata changes are intended:

```bash
bash -lc 'for d in api-stress-test check-folder-size common-module find-content find-everything replace-text; do (cd "$d" && go mod tidy); done'
```

## Focused Verification

- Cobra lifecycle, flags, streams, or exit behavior: run the affected module's `go test ./cmd` and follow `docs/agent/cli-conventions.md`.
- `api-stress-test/` behavior: `cd api-stress-test && env -u NO_COLOR go test ./...`
- `api-stress-test/` lifecycle, pacing, streaming, or cancellation: add `cd api-stress-test && env -u NO_COLOR go test -race ./...` and repeat `go test ./cmd ./internal/request -run 'Test.*(Duration|Grace|Signal|Warmup|RateLimiter|Matcher|Truncated|ExactRequest)' -count=20`
- `api-stress-test/` allocation or performance changes: add `go test ./... -run 'Test.*Allocations' -count=1` and the three-benchmark selector documented above.
- `api-stress-test/internal/request/` request behavior: `cd api-stress-test && go test ./internal/request`
- `api-stress-test/internal/ui/` output/progress behavior: `cd api-stress-test && env -u NO_COLOR go test ./internal/ui`
- `check-folder-size/cmd/` flags, output, timeout, or exit behavior: `cd check-folder-size && go test ./cmd`
- `check-folder-size/internal/scanner/` traversal or progress behavior: `cd check-folder-size && go test ./internal/scanner`
- `check-folder-size/` accounting or concurrency changes: add `cd check-folder-size && go test -race ./...`
- `check-folder-size/` platform metadata or build-tag changes: run scanner tests natively on the target OS and cross-build Darwin, Linux, and Windows with the toolchain declared by `check-folder-size/go.mod`; cross-build is a compile gate, not a substitute for native filesystem tests.
- `find-content/cmd/` search, listing, filters, or CLI behavior: `cd find-content && go test ./cmd`
- `find-content/internal/searcher/` matching, readers, traversal, file policy, ordering, or cancellation: `cd find-content && go test ./internal/searcher`
- `find-content/` concurrency or deterministic-cap changes: add `cd find-content && go test -race ./...` and `cd find-content && go test ./... -run 'Deterministic|Ordering|MaxResults|Coordinator' -count=20`
- `find-content/` matcher changes: add `cd find-content && go test ./... -run '^$' -fuzz '^FuzzMatcher$' -fuzztime=10s`
- `find-everything/cmd/` flags, validation, output routing, or exit behavior: `cd find-everything && go test ./cmd`
- `find-everything/internal/finder/` traversal, result caps, cancellation, partial reports, symlinks, or hidden entries: `cd find-everything && go test ./internal/finder`
- `find-everything/` concurrency-sensitive changes: add `cd find-everything && go test -race ./internal/finder -run 'Test.*(Limit|Queue|Cancel|Partial)' -count=20`
- `find-everything/internal/ui/` large-result behavior: `cd find-everything && go test ./internal/ui`
- `replace-text/cmd/` flags, validation, output, or exit behavior: `cd replace-text && go test ./cmd`
- `replace-text/internal/replacer/` streaming, metadata, backup/rollback, cancellation, or worker behavior: `cd replace-text && go test ./internal/replacer`
- `replace-text/` concurrency or transactional commit changes: add `cd replace-text && go test -race ./...`
- `replace-text/` streaming matcher changes: add `cd replace-text && go test ./internal/replacer -run '^$' -fuzz '^FuzzStreamReplace$' -fuzztime=10s`
- `replace-text/` platform metadata or build-tag changes: cross-build affected targets to `/tmp`, for example `cd replace-text && CGO_ENABLED=0 GOOS=<darwin|linux|windows> GOARCH=amd64 go build -trimpath -o /tmp/replace-text-<os>-amd64 .`
- `common-module/utils/` changes: test/build `check-folder-size`, which imports it.

## Docs Checks

For agent-doc changes:

```bash
git diff --check
find docs/agent -type f -maxdepth 1 -print
```

Also verify links and referenced paths exist when adding new route guidance.
