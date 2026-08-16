package discovery

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/denormal/go-gitignore"
)

func filterCandidate(path string, root string, maxSize int64) (File, SkipReason, string, bool, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return File{}, SkipUnlisted, err.Error(), false, nil
	}

	isSymlink := linkInfo.Mode()&os.ModeSymlink != 0
	info := linkInfo

	if isSymlink {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return File{}, SkipIgnored, resolveErr.Error(), false, nil
		}
		if !isUnderRoot(root, resolved) {
			return File{}, SkipSymlinkEscape, "symlink target escapes canonical root", false, nil
		}

		target, targetErr := os.Stat(path)
		if targetErr != nil {
			return File{}, SkipUnlisted, targetErr.Error(), false, nil
		}
		if target.IsDir() {
			return File{}, SkipDirectorySymlink, "symlink target is directory", false, nil
		}

		info = target
	}

	if info.IsDir() {
		return File{}, SkipDirectorySymlink, "directory entry", false, nil
	}
	if !info.Mode().IsRegular() {
		return File{}, SkipUnsupportedType, fmt.Sprintf("mode %v", info.Mode()), false, nil
	}

	if isSecretPath(path) {
		return File{}, SkipSecret, "secret file policy", false, nil
	}

	if info.Size() > maxSize {
		return File{}, SkipTooLarge, fmt.Sprintf("size %d > %d", info.Size(), maxSize), false, nil
	}

	contents, readErr := os.ReadFile(path)
	if readErr != nil {
		return File{}, SkipUnlisted, readErr.Error(), false, nil
	}
	if bytes.IndexByte(contents, 0) >= 0 {
		return File{}, SkipBinary, "null byte found in file", false, nil
	}
	if !utf8.Valid(contents) {
		return File{}, SkipInvalidUTF8, "invalid UTF-8", false, nil
	}

	language := LanguageForPath(filepath.Base(path))
	if language == "" {
		language = "plain_text"
	}

	return File{
		SizeBytes: info.Size(),
		ModTimeNs: info.ModTime().UnixNano(),
		Language:  language,
	}, "", "", true, nil
}

func shouldSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".hg", ".svn", ".idea", ".vscode", "node_modules", "dist", ".next", "build", "target",
		".cache", ".terraform", "coverage", "vendor", "tmp":
		return true
	default:
		return false
	}
}

func hasDirectorySkips(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts {
		if shouldSkipDir(part) {
			return true
		}
	}
	return false
}

func isUnderRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func toRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	if relative == "." {
		return "."
	}
	return filepath.ToSlash(relative)
}

func isSecretPath(path string) bool {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	return lower == ".env" || strings.HasPrefix(lower, ".env.")
}

func loadIstokIgnore(root string) (gitignore.GitIgnore, error) {
	path := filepath.Join(root, ".istokignore")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var exclusions bytes.Buffer
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// .istokignore is additive: negation cannot restore a file excluded by
		// Git or the built-in policy.
		if strings.HasPrefix(trimmed, "!") {
			continue
		}

		exclusions.WriteString(line)
		exclusions.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return gitignore.New(&exclusions, root, nil), nil
}

func ignoredBy(matcher gitignore.GitIgnore, path string) bool {
	if matcher == nil {
		return false
	}

	// Repository matchers specialize Match, so inspect the returned pattern
	// instead of relying on the embedded convenience method.
	match := matcher.Match(path)
	return match != nil && match.Ignore()
}
