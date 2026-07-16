# Workflows

Run Go commands from an individual module unless the command explicitly loops over modules.

## GitHub Actions CI

`.github/workflows/replace-text-ci.yml` verifies `replace-text` on pushes, pull requests, and manual runs when the workflow or `replace-text/**` changes. GitHub provisions `ubuntu-latest`, `macos-latest`, and `windows-latest`; each job installs Go 1.24.4, then runs:

```bash
go test ./...
go vet ./...
go build -trimpath ./...
```

The public CI mirror is `https://github.com/tuanloc1105/my-cli-tea`. This checkout keeps Gitea as its fetch source and uses multiple `origin` push URLs so one push updates both Gitea and GitHub. That dual-push configuration lives in local Git config; other clones must configure their own GitHub push destination.

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
- `replace-text/cmd/` flags, validation, output, or exit behavior: `cd replace-text && go test ./cmd`
- `replace-text/internal/replacer/` streaming, metadata, backup/rollback, cancellation, or worker behavior: `cd replace-text && go test ./internal/replacer`
- `replace-text/` concurrency or transactional commit changes: add `cd replace-text && go test -race ./...`
- `replace-text/` streaming matcher changes: add `cd replace-text && go test ./internal/replacer -run '^$' -fuzz '^FuzzStreamReplace$' -fuzztime=10s`
- `replace-text/` platform metadata or build-tag changes: cross-build affected targets to `/tmp`, for example `cd replace-text && CGO_ENABLED=0 GOOS=<darwin|linux|windows> GOARCH=amd64 go build -trimpath -o /tmp/replace-text-<os>-amd64 .`
- `common-module/utils/` changes: test/build the three consumers that import it: `case-converter`, `check-folder-size`, and `find-everything`.

## Docs Checks

For agent-doc changes:

```bash
git diff --check
find docs/agent -type f -maxdepth 1 -print
```

Also verify links and referenced paths exist when adding new route guidance.
