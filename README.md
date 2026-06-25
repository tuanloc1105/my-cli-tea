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
Find and replace text in files or directories with automatic backup creation and support for multi-line replacements.

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
```

### Find Content

**Purpose:** Search for text content within files using regex or plain text.

**Key Features:**
- Regex and plain text search
- Multiple file type support
- Directory exclusions
- Line number display

**Usage:**
```bash
# Search for text
./find-content "search term" /path/to/search

# Use regex
./find-content -regex "pattern.*" /path/to/search

# Show line numbers
./find-content -line-nums "term" /path/to/search

# Case insensitive
./find-content -case-insensitive "term" /path/to/search
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

**Purpose:** Find and replace text in files with automatic backup creation.

**Key Features:**
- File and directory processing
- Automatic backup creation (.bak files)
- Multi-line replacement support
- Escape sequence handling

**Usage:**
```bash
# Replace in single file
./replace-text "old text" "new text" file.txt

# Replace in directory
./replace-text "old text" "new text" /path/to/directory

# Handle escape sequences
./replace-text "\\n" "\\r\\n" file.txt
```

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
- **Replace Text:** Batch file modifications with safety backups
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
./find-content "TODO" /path/to/project

# Search for specific function usage
./find-content -case-insensitive "getUser" /path/to/code
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
