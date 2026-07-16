# Project Map

Use this file when a task needs exact source routing. `AGENTS.md` stays the concise index and writable source of truth.

## Repository Shape

- `AGENTS.md` is the repository's agent guide; there is no `CLAUDE.md` source file.
- There is no root Go module. Each tool has its own `go.mod`.
- All five executable entrypoints are minimal wrappers; command construction, streams, errors, and invocation-local options live in each module's `cmd/root.go` as documented in `docs/agent/cli-conventions.md`.
- There is no `docs/agent/` history before this guide set.
- There is no frontend, web app, backend server, database, migration system, container config, or deploy/IaC surface in the repo. CI has GitHub and Gitea workflows for the modules listed under Operational Surfaces.
- `.serena/` is local tooling metadata and is ignored by `.gitignore`; do not treat it as source.

## Modules

| Module | Layout | Purpose | First files |
| --- | --- | --- | --- |
| `api-stress-test/` | Modular Cobra CLI | HTTP load/stress testing | `cmd/root.go`, `internal/request/client.go`, `internal/stats/collector.go`, `internal/ui/output.go` |
| `check-folder-size/` | Modular Cobra CLI | Directory size scanning | `cmd/root.go`, `internal/scanner/types.go`, `internal/scanner/scanner.go`, `internal/scanner/metadata.go`, `internal/ui/printer.go` |
| `find-content/` | Modular Cobra CLI | Deterministic bounded text search and directory listing | `cmd/root.go`, `cmd/render.go`, `internal/searcher/searcher.go`, `internal/searcher/coordinator.go` |
| `find-everything/` | Modular Cobra CLI | File finding and filtering | `cmd/root.go`, `internal/finder/finder.go`, `internal/finder/walker.go`, `internal/ui/display.go` |
| `replace-text/` | Modular Cobra CLI | Streaming find/replace with concurrent traversal and mutation safety | `cmd/root.go`, `internal/replacer/types.go`, `internal/replacer/processor.go`, `internal/replacer/stream.go`, `internal/replacer/metadata.go` |
| `common-module/` | Shared module | Utility helpers | `utils/struct_utils.go`, `utils/system_command_executor.go` |

## Shared Module Usage

Only `check-folder-size/go.mod` currently requires and replaces `common-module`.

Only `check-folder-size/cmd/root.go` currently imports `common-module/utils`.

When changing `common-module/utils/`, verify all consumers, not just the shared module.

## User-Facing Output Surfaces

There is no browser frontend. The product surface is CLI terminal output and JSON/text output:

- `api-stress-test/internal/ui/output.go` and `api-stress-test/internal/ui/progress.go`
- `check-folder-size/cmd/root.go` owns JSON/progress stream routing, partial warnings, and exit behavior; `check-folder-size/internal/ui/printer.go` renders terminal results.
- `find-everything/internal/ui/display.go`
- `find-content/cmd/root.go` and `find-content/cmd/render.go`
- `replace-text/cmd/root.go`

`README.md` is the main user-facing documentation surface for examples and installation notes.

## Data And Side-Effect Paths

- `api-stress-test/cmd/root.go` creates HTTP client/transport behavior, proxy/TLS/redirect/keepalive options, worker fan-out, duration mode, warmup, and output-file handling.
- `api-stress-test/internal/request/client.go` handles headers, form data, JSON/raw/file body input, `http.NewRequestWithContext`, response draining, expected status/body checks, and error normalization.
- `replace-text/cmd/root.go` owns flags, argument handling, user-facing output, and exit codes.
- `replace-text/internal/replacer/processor.go`, `stream.go`, and `metadata*.go` own traversal/concurrency, streaming UTF-8 replacement and size limits, backup/atomic commits, concurrent-change checks, and platform metadata preservation.
- `check-folder-size/internal/scanner/types.go` defines scan/result contracts; `scanner.go` owns traversal, concurrency, cancellation, partial results, exclusions, and symlink/hardlink aggregation; `metadata.go` plus `metadata_<os>.go` own logical/allocated metadata and stable file identity.
- `find-content/internal/searcher/` and `find-everything/internal/finder/` are the other main filesystem traversal/read paths. `find-everything/internal/finder/` owns its bounded queue/local-DFS traversal, exact combined result cap, cancellation cause, partial report, and symlink policy.
- `find-everything/internal/ui/display.go` owns TTY-aware rendering and same-directory temp/rename saves for large result sets.

## Operational Surfaces

- `Makefile` is the build/install/clean entrypoint. Its targets install or move binaries outside the repo.
- Module metadata lives in each module's `go.mod` and `go.sum`.
- `.github/workflows/*-ci.yml` runs checks for `check-folder-size`, `common-module` and its consumer, `find-content`, `find-everything`, and `replace-text` on GitHub-hosted Ubuntu, macOS, and Windows runners. The existing Gitea workflows continue to cover `find-content` and `find-everything` on the same OS families.
- No container files, deploy scripts, env templates, or release automation are present.
- Root `.gitignore` ignores `/plans/` only. Build and test artifacts should be written outside the repository, such as under `/tmp`.
