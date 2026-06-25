# Project Map

Use this file when a task needs exact source routing. `CLAUDE.md` stays the concise index.

## Repository Shape

- `AGENTS.md` is a symlink to `CLAUDE.md`; edit `CLAUDE.md` for agent guide changes.
- There is no root Go module. Each tool has its own `go.mod`.
- There is no `docs/agent/` history before this guide set.
- There is no frontend, web app, backend server, database, migration system, CI config, container config, or deploy/IaC surface in the repo.
- `.serena/` is local tooling metadata and is ignored by `.gitignore`; do not treat it as source.

## Modules

| Module | Layout | Purpose | First files |
| --- | --- | --- | --- |
| `api-stress-test/` | Modular Cobra CLI | HTTP load/stress testing | `cmd/root.go`, `internal/request/client.go`, `internal/stats/collector.go`, `internal/ui/output.go` |
| `case-converter/` | Single-file CLI | Text case conversion | `main.go` |
| `check-folder-size/` | Modular Cobra CLI | Directory size scanning | `cmd/root.go`, `internal/scanner/scanner.go`, `internal/ui/printer.go` |
| `find-content/` | CLI plus search helper | Text search and directory listing | `main.go`, `searcher.go` |
| `find-everything/` | Modular Cobra CLI | File finding and filtering | `cmd/root.go`, `internal/finder/finder.go`, `internal/finder/walker.go`, `internal/ui/display.go` |
| `replace-text/` | Single-file CLI | Find/replace with safety checks | `main.go` |
| `common-module/` | Shared module | Utility helpers | `utils/struct_utils.go`, `utils/system_command_executor.go` |

## Shared Module Usage

Only these modules currently require and replace `common-module`:

- `case-converter/go.mod`
- `check-folder-size/go.mod`
- `find-everything/go.mod`

Only these source files currently import `common-module/utils`:

- `case-converter/main.go`
- `check-folder-size/cmd/root.go`
- `find-everything/cmd/root.go`

When changing `common-module/utils/`, verify all consumers, not just the shared module.

## User-Facing Output Surfaces

There is no browser frontend. The product surface is CLI terminal output and JSON/text output:

- `api-stress-test/internal/ui/output.go` and `api-stress-test/internal/ui/progress.go`
- `check-folder-size/internal/ui/printer.go`
- `find-everything/internal/ui/display.go`
- `case-converter/main.go`
- `find-content/main.go` and `find-content/searcher.go`
- `replace-text/main.go`

`README.md` is the main user-facing documentation surface for examples and installation notes.

## Data And Side-Effect Paths

- `api-stress-test/cmd/root.go` creates HTTP client/transport behavior, proxy/TLS/redirect/keepalive options, worker fan-out, duration mode, warmup, and output-file handling.
- `api-stress-test/internal/request/client.go` handles headers, form data, JSON/raw/file body input, `http.NewRequestWithContext`, response draining, expected status/body checks, and error normalization.
- `replace-text/main.go` is the only general user-file mutation path. It handles binary checks, UTF-8 validation, `.bak` backups, permission preservation, temp files, restore paths, and `filepath.WalkDir`.
- `find-content/searcher.go`, `find-everything/internal/finder/`, and `check-folder-size/internal/scanner/scanner.go` are the main filesystem traversal/read paths.
- `find-everything/internal/ui/display.go` can save large result sets to a file.

## Operational Surfaces

- `Makefile` is the build/install/clean entrypoint. Its targets install or move binaries outside the repo.
- Module metadata lives in each module's `go.mod` and `go.sum`.
- No CI/CD configs, container files, deploy scripts, env templates, or release automation are present.
- `.gitignore` ignores `*.exe` and `.serena/` only. Unix binaries built into tool directories are not ignored.
