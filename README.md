# My CLI Tools Collection

A collection of useful command-line tools written in Go, designed to enhance your development workflow and file management tasks.

## 🛠️ Tools Included

### 1. **Case Converter** (`case-converter/`)
Convert text between various case formats with support for 14 different transformations including snake_case, camelCase, PascalCase, and more.

### 2. **Check Folder Size** (`check-folder-size/`)
Analyze folder sizes with colored output, progress tracking, and customizable exclusions. Perfect for identifying disk space usage.

### 3. **Find Content** (`find-content/`)
Search for text content within files using regex or plain text matching. Supports multiple file types and directory exclusions.

### 4. **Find Everything** (`find-everything/`)
Advanced file and directory finder with pattern matching, size filtering, and parallel processing for large directory trees.

### 5. **Replace Text** (`replace-text/`)
Find and replace UTF-8 text in files or directories with optional backups, dry-run analysis, and bounded-memory streaming.

### 6. **API Stress Test** (`api-stress-test/`)
HTTP load and stress testing tool for APIs with performance metrics collection.

## 🚀 Quick Start

### Prerequisites
- Go 1.24 or later
- Git (for cloning)
- [Powershell 7](https://github.com/PowerShell/PowerShell) - required only if you run these tools in Windows PowerShell. Ignore if you're using Command Prompt.

### Installation

1. **Clone the repository:**
   ```bash
   git clone <repository-url>
   cd my-cli
   ```

2. **Build and install all tools:**
   ```bash
   make all
   ```

3. **Or build individual tools:**
   ```bash
   make case-converter
   make check-folder-size
   make find-content
   make find-everything
   make replace-text
   make api-stress-test
   ```

   The Makefile supports both Linux and Windows (PowerShell 7 with MSYS2 `make`).

## 📖 Tool Documentation

### Case Converter

**Purpose:** Convert text between various case formats for programming and documentation.

**Key Features:**
- 14 different case formats (snake_case, camelCase, PascalCase, etc.)
- Automatic detection of input format
- Colored terminal output
- File input support

**Usage:**
```bash
# Basic conversion
./case-converter "hello world"

# From file
./case-converter -f input.txt

# Specific format only
./case-converter "hello world" --format snake
```

**Supported Formats:**
- `normal`, `upper`, `lower`, `capitalized`, `swapped`
- `snake_case`, `kebab-case`, `camel_case`, `pascal_case`
- `constant_case`, `title_case`, `dot_case`, `path_case`, `pascal_kebab`

### Check Folder Size

**Purpose:** Analyze disk usage and identify large files/folders.

**Key Features:**
- Colored output based on size (green/yellow/red)
- Progress tracking for large directories
- Customizable exclusions
- Sort by size or name
- Native allocated disk usage on macOS, Linux, and Windows; logical size remains available
- Deterministic hardlink accounting, valid JSON output, and explicit partial-scan failures

**Usage:**
```bash
# Analyze current directory
./check-folder-size

# Analyze with progress
./check-folder-size -progress

# Exclude specific folders
./check-folder-size -exclude-dirs "node_modules,.git"

# Sort by name
./check-folder-size -sort name -asc

# Report apparent/logical bytes instead of allocated disk usage
./check-folder-size --size-mode logical

# Produce machine-readable output without progress or ANSI contamination
./check-folder-size --json
```

`--size-mode allocated` is the default. It uses native allocated bytes and includes
the metadata blocks of displayed directories and their descendants; the scan
root's own blocks are excluded so the reported total equals the sum of displayed
items. `--size-mode logical` reports apparent file bytes and preserves the
logical-size use case. Displayed units and `B`/`KB`/`MB`/`GB`/`TB` filters use
binary multipliers (base 1024).

Hidden names are scanned normally, including dot entries on POSIX and entries
with the Windows hidden attribute. Nested symlinks and broken symlinks are
measured as links and never followed; FIFO, socket, device, and other special
entries are measured as their own metadata and reported as `other`. A root
symlink to a directory is accepted as an input convenience.

In allocated mode, observed regular-file hardlinks are counted once across the
entire scan and attributed to the lexicographically smallest top-level path.
Links outside the scanned tree cannot be discovered, and reflinks/filesystem
clones may share physical extents even though they have distinct file identities.

Successful JSON output is exactly one array (`[]` when empty). Warnings and
explicit JSON progress go to stderr. Fatal validation/root-open errors and
partial scans exit with status 1; partial scans still emit the data collected so
far. Timeouts are cooperative: filesystem work checks cancellation frequently,
but an operating-system `ReadDir` call already blocked in the kernel cannot be
interrupted immediately.

### Find Content

**Purpose:** Search for text content within files using regex or plain text.

**Key Features:**
- Regex and plain text search
- Deterministic bounded-concurrency output and exact result caps
- Hidden-entry, binary, symlink, and special-file policies
- Normal streaming and bounded multiline search
- Directory listing with OS-aware hidden detection

**Usage:**
```bash
# Search for text
./find-content /path/to/search "search term"

# Use regex
./find-content --regex /path/to/search "pattern.*"

# Search only selected extensions
./find-content --extensions go,md /path/to/search "term"

# List a directory, including hidden entries
./find-content --list --show-hidden /path/to/search
```

### Find Everything

**Purpose:** Advanced file and directory finder with pattern matching and filtering.

**Key Features:**
- Pattern-based file/directory matching
- Size filtering (min/max)
- File type filtering
- Parallel processing
- Progress tracking

**Usage:**
```bash
# Find files by pattern
./find-everything "*.txt" /path/to/search

# Filter by size
./find-everything -min-size "1MB" -max-size "100MB" "*.log" /path

# Find specific file types
./find-everything -file-types "go,js,py" "*" /path

# Show progress
./find-everything -progress "*.md" /path
```

### Replace Text

**Purpose:** Find and replace UTF-8 text in files and directory trees with explicit safety and resource limits.

**Key Features:**
- File and directory processing
- Optional `.bak` creation with `--backup` (backups are not created by default)
- Multi-line replacement with `\n`, `\r`, `\t`, and `\\` auto-unescaping
- Literal and dry-run modes
- Bounded worker concurrency and streaming input/output limits
- Exact no-match and policy-skip reasons for a single target

**Usage:**
```bash
# Replace in single file
./replace-text "old text" "new text" file.txt

# Replace in directory
./replace-text "old text" "new text" /path/to/directory

# Handle escape sequences
./replace-text "\\n" "\\r\\n" file.txt

# Preserve backslashes instead of interpreting escape sequences
./replace-text --literal "\\n" "line break" file.txt

# Count replacements without changing files or creating backups
./replace-text --dry-run "old text" "new text" /path/to/directory

# Create a backup only for files that are modified
./replace-text --backup "old text" "new text" file.txt

# Separate flags from text that starts with a dash
./replace-text -- "--old" "--new" file.txt
```

**Flags:**
- `--backup`: create a `.bak` copy for each modified file; disabled by default.
- `--max-size <bytes>`: maximum input size per file; defaults to 512 MiB (`536870912` bytes).
- `--literal`: do not interpret `\n`, `\r`, `\t`, or `\\` in the two text arguments.
- `--dry-run`: analyze and count replacements without creating temp files, backups, or metadata changes.
- `--max-workers <count>`: maximum concurrent file workers; defaults to `min(runtime.NumCPU(), 8)` and must be at least `1`.
- `--max-output-size <bytes>`: maximum output size per file; `0` (the default) means unlimited and negative values are usage errors.

**Output and exit status:** A modified file keeps the compatibility line `Successfully replaced text in '<path>'.`. A single no-match or policy skip prints its exact human-readable reason. Directory runs retain per-file success/error lines, finish with `Finished processing directory '<path>'.`, and print fixed-order summary counters plus a fixed-order skip breakdown. Usage errors, operational errors, and partial directory failures return `1`; no-match and policy skips return `0`.

**Streaming and limits:** Replacement uses a two-pass streaming pipeline rather than loading the entire file. Per-worker memory is bounded by `O(max(64 KiB, len(old-text)))`, and recursive traversal uses bounded channels instead of collecting every path or result. Files that exceed the input or projected-output policy limit are left unchanged and reported as skips. Only valid UTF-8 regular files with one hard link are modified; NUL/binary data, invalid UTF-8, symlinks, hardlinks, and non-regular entries are skipped with a reason.

**Platform and metadata limitations:** On macOS and Linux, the tool preserves supported mode, ownership, access time, and modification time or aborts before commit when required preservation cannot be completed. Windows mode/time handling is best-effort, and owner SID preservation is not guaranteed. ACLs, extended attributes, resource forks, Linux capabilities and SELinux labels, sparse/reflink layout, and birth time are not preserved. Replacement rename is described as atomic only on macOS/Linux local filesystems; Windows, network, and removable filesystems are best-effort. Linux and Windows runtime semantics must be verified on real target machines or runners before release; cross-compilation alone verifies only that the code builds.

## 🎯 Use Cases

### Development Workflow
- **Case Converter:** Standardize variable names, file names, and documentation
- **Find Content:** Search for function usage, API endpoints, or configuration values
- **Replace Text:** Bulk refactoring, configuration updates, or text standardization

### System Administration
- **Check Folder Size:** Monitor disk usage, identify large files
- **Find Everything:** Locate specific file types, audit file systems
- **Find Content:** Search logs, configuration files, or documentation

### File Management
- **Find Everything:** Organize files by type, size, or pattern
- **Replace Text:** Batch file modifications with optional safety backups
- **Check Folder Size:** Clean up disk space efficiently

## 🔧 Development

### Project Structure
```
my-cli/
├── case-converter/     # Text case conversion tool
├── check-folder-size/  # Disk usage analyzer
├── find-content/       # Text search tool
├── find-everything/    # Advanced file finder
├── replace-text/       # Text replacement tool
├── api-stress-test/    # API load/stress testing tool
├── common-module/      # Shared utilities
└── Makefile            # Build & install (Linux + Windows)
```

### Building from Source
Each tool is self-contained with its own `go.mod` file and references `common-module` via a local `replace` directive.

Use the Makefile to build and install:
```bash
# Build all
make all

# Build one tool
make find-content

# Clean build artifacts
make clean
```

### Dependencies
All tools use:
- **Cobra:** CLI framework for Go
- **Standard library:** File operations, regex, unicode handling

## 📝 Examples

### Case Conversion Workflow
```bash
# Convert API endpoint names
./case-converter "user authentication endpoint" --format snake
# Output: user_authentication_endpoint

# Convert to PascalCase for class names
./case-converter "database connection manager" --format pascal
# Output: DatabaseConnectionManager
```

### Disk Usage Analysis
```bash
# Find largest directories
./check-folder-size -sort size -asc

# Exclude build artifacts
./check-folder-size -exclude-dirs "node_modules,dist,build,.git"
```

### Content Search
```bash
# Find all TODO comments
./find-content /path/to/project "TODO"

# Search for specific function usage
./find-content --regex /path/to/code 'get(User|Account)'
```

### File Discovery
```bash
# Find all configuration files
./find-everything "*.{json,yaml,yml,ini}" /path/to/config

# Find large log files
./find-everything -min-size "10MB" "*.log" /var/log
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📄 License

This project is open source and available under the MIT License.

## 🆘 Support

For issues, feature requests, or questions:
1. Check the individual tool READMEs for specific documentation
2. Review the examples above
3. Open an issue on the repository

---

**Happy coding! 🚀** 
