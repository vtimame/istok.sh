package discovery

import (
	"path/filepath"
	"strings"
)

// LanguageForPath derives a coarse language label from path.
func LanguageForPath(path string) string {
	lowerExt := strings.ToLower(filepath.Ext(path))
	switch lowerExt {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx", ".c++", ".hpp", ".hh", ".h":
		return "cpp"
	case ".rs":
		return "rust"
	case ".md", ".markdown":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".svelte":
		return "svelte"
	}

	base := strings.TrimSpace(strings.ToLower(filepath.Base(path)))
	switch base {
	case "makefile":
		return "makefile"
	case "dockerfile":
		return "dockerfile"
	case "go.mod", "go.sum", "go.work":
		return "go"
	case ".gitignore", ".istokignore":
		return "gitignore"
	}
	return "plain_text"
}
