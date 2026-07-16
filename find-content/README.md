# find-content

`find-content` searches regular text files recursively with deterministic output, bounded concurrency, and explicit resource/error policies. It can also list one directory.

## Build

Run commands from this module because the repository has no top-level Go module:

```bash
go build -o /tmp/find-content .
go test ./...
```

Avoid the repository `Makefile` when you only need a local build; its targets install or move binaries outside the repository.

## Search

```bash
find-content [flags] <directory> <keyword>
```

Examples:

```bash
find-content ./src "TODO"
find-content --regex ./src 'func\s+Run'
find-content --extensions go,md ./src "context"
find-content --multiline ./src 'first line\nsecond line'
find-content --max-results 20 --max-workers 4 ./src "error"
find-content ./src -- "--literal-keyword"
```

Search includes dot-prefixed files and directories and does not exclude entries merely because the OS marks them hidden. Built-in directory excludes are `.git`, `__pycache__`, `node_modules`, `.vscode`, `.idea`, `target`, `build`, and `dist`; an explicitly supplied root is never removed by this built-in list. Use `--no-default-excludes` to disable the list. Values supplied through `--exclude-dirs` and `--exclude-files` still apply.

Results are ordered bytewise by slash-normalized relative path, then by line/byte offset within each file. `--max-results N` emits exactly the first `N` ordered results and cancels remaining work; `0` means unlimited and negative values are usage errors.

Normal mode streams the whole file one line at a time. `--max-line-size` defaults to 64 MiB per line; an oversized line is discarded, reported as a partial error, and processing continues at the next line. Multiline mode reads at most `--max-multiline-size` plus one byte to enforce its 64 MiB default; a file exactly at the limit is accepted and a larger file is a partial error. `--max-workers` defaults to `min(runtime.NumCPU(), 4)` and must be at least `1`.

Literal matching is case-insensitive by default and preserves byte offsets on the original UTF-8 content. Use `--case-sensitive` for exact case or `--regex` for Go regular expressions. Normal mode emits one result per matching line; multiline mode emits one result per non-overlapping match/range.

## File policy

- Only regular files are opened. File and directory symlinks, FIFOs, sockets, devices, and other special entries are skipped without warnings.
- A symlink supplied as the root is rejected.
- Every candidate receives a NUL-byte preview; NUL-binary files are silently skipped, including with `--all`.
- `--extensions` is authoritative and strict. Comma-separated values are trimmed, empty values are removed, and duplicates are ignored.
- Without `--extensions`, common text extensions and basenames such as `Makefile`, `Dockerfile`, `README`, `.env*`, and dotfile configs are recognized.
- `--all` bypasses the filename classifier but not regular-file or NUL-binary checks. It cannot be combined with `--extensions`.

## List mode

Canonical syntax is:

```bash
find-content --list <directory>
find-content --list --show-hidden <directory>
```

List mode hides hidden entries by default. Dot-prefixed names are hidden on every OS; Windows hidden attributes and the macOS `UF_HIDDEN` flag are also honored. The legacy two-positional-argument form remains accepted temporarily and prints a deprecation warning. Search-only flags are rejected in list mode.

## Output and exit status

| Code | Meaning |
| --- | --- |
| `0` | Search completed with at least one match, or list/help completed successfully. |
| `1` | Search completed cleanly with no matches. |
| `2` | Usage, validation, root, regex, writer, traversal, read, or resource-limit error. |

Per-path failures are warnings and do not discard valid matches from other files, but the final exit status is `2`. `--suppress-warnings` hides only those per-path warning lines; it does not hide the final incomplete-search error or change the exit status. `No matches found` is printed only for a clean no-match search.
