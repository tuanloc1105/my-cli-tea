# Project Map

Use this file when a task needs exact source routing. `AGENTS.md` stays the concise index and writable source of truth.

## Repository Shape

- `AGENTS.md` is the repository's agent guide; there is no `CLAUDE.md` source file.
- There is no root Go module. Each tool has its own `go.mod`.
- All six executable entrypoints are minimal wrappers; command construction, streams, errors, and invocation-local options live in each module's `cmd/root.go` as documented in `docs/agent/cli-conventions.md`.
- There is no `docs/agent/` history before this guide set.
- There is no frontend, web app, backend server, database, migration system, container config, or deploy/IaC surface in the repo. CI is limited to the `replace-text` GitHub Actions workflow.
- `.serena/` is local tooling metadata and is ignored by `.gitignore`; do not treat it as source.

## Modules

| Module | Layout | Purpose | First files |
| --- | --- | --- | --- |
| `api-stress-test/` | Modular Cobra CLI | HTTP load/stress testing | `cmd/root.go`, `internal/request/client.go`, `internal/stats/collector.go`, `internal/ui/output.go` |
| `case-converter/` | Modular Cobra CLI | Text case conversion | `cmd/root.go`, `cmd/converter.go` |
| `check-folder-size/` | Modular Cobra CLI | Directory size scanning | `cmd/root.go`, `internal/scanner/scanner.go`, `internal/ui/printer.go` |
| `find-content/` | Modular Cobra CLI | Text search and directory listing | `cmd/root.go`, `cmd/searcher.go` |
| `find-everything/` | Modular Cobra CLI | File finding and filtering | `cmd/root.go`, `internal/finder/finder.go`, `internal/finder/walker.go`, `internal/ui/display.go` |
| `replace-text/` | Modular Cobra CLI | Streaming find/replace with concurrent traversal and mutation safety | `cmd/root.go`, `internal/replacer/types.go`, `internal/replacer/processor.go`, `internal/replacer/stream.go`, `internal/replacer/metadata.go` |
| `common-module/` | Shared module | Utility helpers | `utils/struct_utils.go`, `utils/system_command_executor.go` |

## Shared Module Usage

Only these modules currently require and replace `common-module`:

- `case-converter/go.mod`
- `check-folder-size/go.mod`
- `find-everything/go.mod`

Only these source files currently import `common-module/utils`:

- `case-converter/cmd/root.go`
- `check-folder-size/cmd/root.go`
- `find-everything/cmd/root.go`

When changing `common-module/utils/`, verify all consumers, not just the shared module.

## User-Facing Output Surfaces

There is no browser frontend. The product surface is CLI terminal output and JSON/text output:

- `api-stress-test/internal/ui/output.go` and `api-stress-test/internal/ui/progress.go`
- `check-folder-size/internal/ui/printer.go`
- `find-everything/internal/ui/display.go`
- `case-converter/cmd/converter.go`
- `find-content/cmd/root.go` and `find-content/cmd/searcher.go`
- `replace-text/cmd/root.go`

`README.md` is the main user-facing documentation surface for examples and installation notes.

## Data And Side-Effect Paths

- `api-stress-test/cmd/root.go` creates HTTP client/transport behavior, proxy/TLS/redirect/keepalive options, worker fan-out, duration mode, warmup, and output-file handling.
- `api-stress-test/internal/request/client.go` handles headers, form data, JSON/raw/file body input, `http.NewRequestWithContext`, response draining, expected status/body checks, and error normalization.
- `replace-text/cmd/root.go` owns flags, argument handling, user-facing output, and exit codes.
- `replace-text/internal/replacer/processor.go`, `stream.go`, and `metadata*.go` own traversal/concurrency, streaming UTF-8 replacement and size limits, backup/atomic commits, concurrent-change checks, and platform metadata preservation.
- `find-content/cmd/searcher.go`, `find-everything/internal/finder/`, and `check-folder-size/internal/scanner/scanner.go` are the main filesystem traversal/read paths.
- `find-everything/internal/ui/display.go` can save large result sets to a file.

## Operational Surfaces

- `Makefile` is the build/install/clean entrypoint. Its targets install or move binaries outside the repo.
- Module metadata lives in each module's `go.mod` and `go.sum`.
- `.github/workflows/replace-text-ci.yml` runs `replace-text` tests, vet, and build checks on GitHub-hosted Ubuntu, macOS, and Windows runners.
- No container files, deploy scripts, env templates, or release automation are present.
- Root `.gitignore` ignores `/plans/` only. Build and test artifacts should be written outside the repository, such as under `/tmp`.
