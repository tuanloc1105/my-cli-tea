package searcher

import (
	"path/filepath"
	"strings"
)

var knownTextExtensions = map[string]struct{}{
	".txt": {}, ".md": {}, ".py": {}, ".js": {}, ".ts": {}, ".html": {},
	".css": {}, ".scss": {}, ".json": {}, ".xml": {}, ".yaml": {}, ".yml": {},
	".ini": {}, ".cfg": {}, ".conf": {}, ".sh": {}, ".bash": {}, ".sql": {},
	".java": {}, ".cpp": {}, ".c": {}, ".h": {}, ".hpp": {}, ".cs": {},
	".php": {}, ".rb": {}, ".go": {}, ".rs": {}, ".swift": {}, ".kt": {},
	".scala": {}, ".r": {}, ".m": {}, ".pl": {}, ".lua": {}, ".dart": {},
	".vue": {}, ".jsx": {}, ".tsx": {}, ".properties": {}, ".log": {},
	".toml": {}, ".csv": {}, ".tsv": {}, ".graphql": {}, ".gql": {},
	".proto": {}, ".mod": {}, ".sum": {}, ".lock": {}, ".gradle": {},
	".groovy": {}, ".tex": {}, ".rst": {}, ".adoc": {},
}

var knownTextBasenames = map[string]struct{}{
	"Makefile": {}, "GNUmakefile": {}, "Dockerfile": {}, "Containerfile": {},
	"Jenkinsfile": {}, "Procfile": {}, "Gemfile": {}, "Rakefile": {},
	"README": {}, "LICENSE": {}, "NOTICE": {}, "CHANGELOG": {}, ".env": {},
	".gitignore": {}, ".gitattributes": {}, ".gitmodules": {}, ".editorconfig": {},
	".dockerignore": {}, ".npmrc": {}, ".yarnrc": {}, ".babelrc": {},
	".eslintrc": {}, ".prettierrc": {}, ".bashrc": {}, ".zshrc": {}, ".profile": {},
}

type classifier struct {
	searchAll  bool
	extensions map[string]struct{}
}

func newClassifier(searchAll bool, extensions []string) classifier {
	result := classifier{searchAll: searchAll, extensions: make(map[string]struct{}, len(extensions))}
	for _, extension := range extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		result.extensions[extension] = struct{}{}
	}
	return result
}

func (c classifier) accepts(path string) bool {
	if c.searchAll {
		return true
	}
	extension := strings.ToLower(filepath.Ext(path))
	if len(c.extensions) > 0 {
		_, ok := c.extensions[extension]
		return ok
	}
	if _, ok := knownTextExtensions[extension]; ok {
		return true
	}
	base := filepath.Base(path)
	if _, ok := knownTextBasenames[base]; ok {
		return true
	}
	return strings.HasPrefix(base, ".env.")
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
