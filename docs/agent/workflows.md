# Workflows

Run Go commands from an individual module unless the command explicitly loops over modules.

## Local Build

Build one module without installing:

```bash
cd <tool-dir> && go build -o <tool-name> .
```

Build all CLI modules locally without using the install-oriented `Makefile`:

```bash
bash -lc 'for d in api-stress-test case-converter check-folder-size find-content find-everything replace-text; do (cd "$d" && go build -o "$(basename "$d")" .); done'
```

The root `Makefile` uses `CGO_ENABLED=0` and platform-specific install paths. Prefer the local build loop when you only need compile verification.

## Makefile

Available targets:

- `make all`
- `make case-converter`
- `make check-folder-size`
- `make find-content`
- `make find-everything`
- `make replace-text`
- `make api-stress-test`
- `make clean`

Install/move behavior:

- macOS: installs to `$(HOME)/dev-kit/tool`; `case-converter` installs as `c`.
- Linux: installs to `/usr/local/bin` via `sudo mv`; `case-converter` installs as `c`.
- Windows/MSYS: installs to `D:/dev-kit/tool`; `case-converter` installs as `case-converter.exe`.

Use `make clean` after local builds if generated binaries are not needed. `.gitignore` ignores `*.exe` but not Unix binary names.

## Tests

Test one module:

```bash
cd <tool-dir> && go test ./...
```

Test all modules:

```bash
bash -lc 'for d in api-stress-test case-converter check-folder-size common-module find-content find-everything replace-text; do (cd "$d" && go test ./...); done'
```

Run the focused benchmark currently present in the repo:

```bash
cd api-stress-test && go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem
```

## Vet, Format, Tidy

Vet all modules:

```bash
bash -lc 'for d in api-stress-test case-converter check-folder-size common-module find-content find-everything replace-text; do (cd "$d" && go vet ./...); done'
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
bash -lc 'for d in api-stress-test case-converter check-folder-size common-module find-content find-everything replace-text; do (cd "$d" && go mod tidy); done'
```

## Focused Verification

- `api-stress-test/` behavior: `cd api-stress-test && go test ./...`
- `api-stress-test/internal/stats/` performance or percentile changes: add `go test ./internal/stats -bench BenchmarkCollectorRecord -benchmem`
- `api-stress-test/internal/request/` request behavior: `cd api-stress-test && go test ./internal/request`
- `api-stress-test/internal/ui/` output/progress behavior: `cd api-stress-test && go test ./internal/ui`
- `find-everything/internal/ui/` large-result behavior: `cd find-everything && go test ./internal/ui`
- `common-module/utils/` changes: test/build the three consumers that import it: `case-converter`, `check-folder-size`, and `find-everything`.

## Docs Checks

For agent-doc changes:

```bash
git diff --check
find docs/agent -type f -maxdepth 1 -print
```

Also verify links and referenced paths exist when adding new route guidance.
