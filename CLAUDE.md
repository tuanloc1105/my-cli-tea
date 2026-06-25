# Agent Guide

`AGENTS.md` is a symlink to this file. Treat `CLAUDE.md` as the writable source of truth for agent-facing instructions in this repository.

## Start Here

- Use `/Users/locvotuan/.codex/RTK.md`: prefix shell commands with `rtk`; use `rtk proxy <cmd>` only when exact unfiltered output is required.
- Prefer `context-mode` MCP for large command output, broad searches, logs, generated analysis, and web fetches. Do not use raw `curl` or `wget`.
- Prefer Serena symbolic retrieval for Go source structure before broad source-file reads.
- For repo edits, read current files first and keep changes surgical. Do not revert unrelated user changes.
- This repo has no top-level `go.mod`; run Go commands from the individual module directory unless using an explicit loop.

## Project Map

This is a collection of six standalone Go CLI tools plus one shared module. Each tool has its own module and builds independently.

| Area                   | Purpose                                                                                                | Read first                                                                           |
| ---------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `api-stress-test/`   | HTTP load/stress tester. Current active project focus is high-concurrency correctness and performance. | `api-stress-test/cmd/root.go`, then `docs/agent/api-stress-test.md`              |
| `case-converter/`    | Text case conversion CLI.                                                                              | `case-converter/main.go`                                                           |
| `check-folder-size/` | Directory size analyzer with terminal and JSON output.                                                 | `check-folder-size/cmd/root.go`, `check-folder-size/internal/scanner/scanner.go` |
| `find-content/`      | Text search CLI with regex/plain, multiline, filtering, and listing modes.                             | `find-content/main.go`, `find-content/searcher.go`                               |
| `find-everything/`   | File finder with pattern, size, type, progress, and large-result handling.                             | `find-everything/cmd/root.go`, `find-everything/internal/finder/finder.go`       |
| `replace-text/`      | Find/replace CLI with binary checks, optional backups, and atomic writes.                              | `replace-text/main.go`                                                             |
| `common-module/`     | Shared utilities used by `case-converter`, `check-folder-size`, and `find-everything`.           | `common-module/utils/`                                                             |

For detailed package routing, read `docs/agent/project-map.md`.

## Common Workflows

- Build one tool locally without installing:

```bash
cd <tool-dir> && rtk go build -o <tool-name> .
```

- Test one module:

```bash
cd <tool-dir> && rtk go test ./...
```

- Run the focused `api-stress-test` benchmark:

```bash
cd api-stress-test && rtk go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem
```

- Build or install through `Makefile` only when that is intended. `make` targets move binaries outside the repo: macOS to `$(HOME)/dev-kit/tool`, Linux to `/usr/local/bin`, Windows/MSYS to `D:/dev-kit/tool`.
- Clean build artifacts with `rtk make clean`. Unix binaries produced inside tool directories are not ignored by `.gitignore`.

For all-module loops, Makefile caveats, tidy, vet, and release notes, read `docs/agent/workflows.md`.

## Task Routing

- Changing CLI flags or command validation: read the tool's `cmd/root.go` when present, otherwise `main.go`.
- Changing terminal output or JSON output: read the relevant `internal/ui/` package or single-file CLI output functions first.
- Changing `api-stress-test` HTTP behavior: read `api-stress-test/cmd/root.go`, `api-stress-test/internal/request/client.go`, and `api-stress-test/internal/request/ratelimiter.go`.
- Changing `api-stress-test` metrics, percentiles, histograms, throughput, or high-concurrency behavior: read `api-stress-test/internal/stats/collector.go` and `docs/agent/api-stress-test.md`.
- Changing filesystem traversal/search: read `check-folder-size/internal/scanner/scanner.go`, `find-content/searcher.go`, or `find-everything/internal/finder/` as appropriate.
- Changing file mutation safety: read `replace-text/main.go` first.
- Changing shared utilities: read `common-module/utils/`, then build/test every consumer that imports `common-module/utils`.
- Changing tests or verification strategy: read `docs/agent/testing.md`.
- Changing build/install behavior: read `Makefile` and `docs/agent/workflows.md`.

## Conventions And Guardrails

- Go version target is Go 1.24. Keep dependencies minimal; Cobra is the CLI framework used across tools.
- Use standard Go formatting. Run `rtk gofmt` or `rtk gofmt -w` only on files you intentionally changed.
- Error handling should use contextual `fmt.Errorf("...: %w", err)` where wrapping helps callers.
- Preserve CLI flag names and existing public behavior unless the user explicitly asks for a breaking change.
- File safety matters: keep binary detection, UTF-8 validation, backup restore paths, and temp-file rename behavior intact.
- Concurrency patterns use goroutines, `sync.WaitGroup`, channels, mutexes, and atomics. For high-concurrency `api-stress-test` changes, verify the tool does not bottleneck on its own collectors, progress UI, or request setup.
- Output uses direct ANSI escape codes and standard `fmt` output; there is no structured logging library.

## Read On Demand

- `docs/agent/project-map.md` - detailed module/package map and first-read files.
- `docs/agent/workflows.md` - build, test, tidy, vet, Makefile, install, and cleanup workflows.
- `docs/agent/testing.md` - current test coverage, gaps, and focused verification commands.
- `docs/agent/api-stress-test.md` - high-concurrency stress-test architecture, data flow, performance risks, and verification.
