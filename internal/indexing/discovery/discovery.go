// Package discovery provides deterministic file discovery for local indexing.
package discovery

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/denormal/go-gitignore"
)

const (
	maxFileSizeBytes = 512 * 1024
)

// MaxFileSizeBytes controls oversized filtering and is exported for tests.
const MaxFileSizeBytes = maxFileSizeBytes

const (
	// SkipHidden is reserved for dependency and build cache directories.
	SkipHidden SkipReason = "hidden"
	// SkipIgnored is returned for .gitignore matches.
	SkipIgnored SkipReason = "ignored"
	// SkipSecret is returned for .env and .env.* paths.
	SkipSecret SkipReason = "secret"
	// SkipSymlinkEscape means the link target resolves outside canonical root.
	SkipSymlinkEscape SkipReason = "symlink_escape"
	// SkipDirectorySymlink means a directory symlink was encountered.
	SkipDirectorySymlink SkipReason = "directory_symlink"
	// SkipUnsupportedType indicates a file was not regular.
	SkipUnsupportedType SkipReason = "unsupported_file_type"
	// SkipTooLarge indicates size limit was exceeded.
	SkipTooLarge SkipReason = "oversized"
	// SkipBinary indicates binary content.
	SkipBinary SkipReason = "binary"
	// SkipInvalidUTF8 indicates invalid UTF-8 content.
	SkipInvalidUTF8 SkipReason = "invalid_utf8"
	// SkipUnlisted is an internal fallback reason for read/inspection errors.
	SkipUnlisted SkipReason = "unlisted"
)

// SkipReason explains why a path was skipped.
type SkipReason string

// File describes one candidate file for indexing.
type File struct {
	Path      string
	AbsPath   string
	SizeBytes int64
	ModTimeNs int64
	Language  string
}

// Diagnostic is emitted for every skipped path.
type Diagnostic struct {
	Path   string
	Reason SkipReason
	Detail string
}

// Result contains discovered files and skip diagnostics.
type Result struct {
	Files       []File
	Diagnostics []Diagnostic
}

// Discover enumerates files in root for indexing.
func Discover(ctx context.Context, root string) (Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return Result{}, fmt.Errorf("resolve root symlinks: %w", err)
	}

	inGit, err := isGitWorktree(ctx, canonicalRoot)
	if err != nil {
		inGit = false
	}
	if inGit {
		return discoverByGit(ctx, canonicalRoot, maxFileSizeBytes)
	}

	return discoverByWalker(ctx, canonicalRoot, maxFileSizeBytes)
}

func isGitWorktree(ctx context.Context, root string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--is-inside-work-tree") //nolint:gosec
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "true", nil
}

func discoverByGit(ctx context.Context, root string, maxSize int64) (Result, error) {
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		root,
		"ls-files",
		"-z",
		"--cached",
		"--others",
		"--exclude-standard",
		"--deduplicate",
		"--",
		".",
	) //nolint:gosec
	raw, err := cmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("run git ls-files: %w", err)
	}

	parts := bytes.Split(raw, []byte{0})
	istokIgnore, err := loadIstokIgnore(root)
	if err != nil {
		return Result{}, fmt.Errorf("load .istokignore: %w", err)
	}
	ignored, err := discoverIgnoredByGit(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("run git ignored ls-files: %w", err)
	}
	ignoredSet := make(map[string]struct{}, len(ignored))
	for _, path := range ignored {
		ignoredSet[path] = struct{}{}
	}

	files := make([]File, 0, len(parts))
	diagnostics := make([]Diagnostic, 0)

	for _, part := range parts {
		if len(part) == 0 {
			continue
		}

		relative := filepath.FromSlash(string(part))
		relative = filepath.Clean(relative)
		if relative == "." {
			continue
		}
		absolute := filepath.Join(root, relative)
		if ignoredBy(istokIgnore, absolute) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   filepath.ToSlash(relative),
				Reason: SkipIgnored,
				Detail: "ignored by .istokignore",
			})
			continue
		}
		if hasDirectorySkips(filepath.ToSlash(relative)) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   filepath.ToSlash(relative),
				Reason: SkipHidden,
				Detail: "dependency/build/cache directory",
			})
			continue
		}

		file, reason, detail, keep, err := filterCandidate(absolute, root, maxSize)
		if err != nil {
			return Result{}, err
		}
		if !keep {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   filepath.ToSlash(relative),
				Reason: reason,
				Detail: detail,
			})
			continue
		}

		file.Path = filepath.ToSlash(relative)
		file.AbsPath = absolute
		files = append(files, file)
	}

	for path := range ignoredSet {
		diagnostics = append(diagnostics, Diagnostic{
			Path:   filepath.ToSlash(path),
			Reason: SkipIgnored,
			Detail: "ignored by .gitignore",
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Reason < diagnostics[j].Reason
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return Result{Files: files, Diagnostics: diagnostics}, nil
}

func discoverIgnoredByGit(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		root,
		"ls-files",
		"-z",
		"-o",
		"-i",
		"--exclude-standard",
		"--directory",
		"--deduplicate",
		"--",
		".",
	) //nolint:gosec
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := bytes.Split(raw, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}

		relative := filepath.FromSlash(string(part))
		relative = filepath.Clean(relative)
		if relative == "." || strings.HasSuffix(relative, string(os.PathSeparator)) {
			continue
		}
		result = append(result, relative)
	}
	sort.Strings(result)
	return result, nil
}

func discoverByWalker(ctx context.Context, root string, maxSize int64) (Result, error) {
	ignore, err := gitignore.NewRepository(root)
	if err != nil {
		return Result{}, fmt.Errorf("build gitignore matcher: %w", err)
	}

	istokIgnore, err := loadIstokIgnore(root)
	if err != nil {
		return Result{}, fmt.Errorf("load .istokignore: %w", err)
	}

	files := make([]File, 0)
	diagnostics := make([]Diagnostic, 0)

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if walkErr != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   filepath.ToSlash(toRelative(root, path)),
				Reason: SkipUnlisted,
				Detail: walkErr.Error(),
			})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if path == root {
			return nil
		}

		relative := filepath.ToSlash(toRelative(root, path))
		if relative == "" || relative == "." {
			return nil
		}

		isDir := entry.IsDir()
		mode := entry.Type()
		if mode&os.ModeSymlink != 0 {
			isDir = false
			if info, err := os.Stat(path); err == nil {
				isDir = info.IsDir()
			}
		}

		if isDir {
			if shouldSkipDir(filepath.Base(path)) {
				diagnostics = append(diagnostics, Diagnostic{
					Path:   relative,
					Reason: SkipHidden,
					Detail: "dependency/build/cache directory",
				})
				return filepath.SkipDir
			}

			if mode&os.ModeSymlink != 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:   relative,
					Reason: SkipDirectorySymlink,
					Detail: "directory symlink is not followed",
				})
				return filepath.SkipDir
			}

			// .git directories are intentionally skipped as a dependency directory.
			if ignoredBy(ignore, path) {
				diagnostics = append(diagnostics, Diagnostic{
					Path:   relative,
					Reason: SkipIgnored,
					Detail: "ignored by .gitignore",
				})
				return filepath.SkipDir
			}
			if ignoredBy(istokIgnore, path) {
				diagnostics = append(diagnostics, Diagnostic{
					Path:   relative,
					Reason: SkipIgnored,
					Detail: "ignored by .istokignore",
				})
				return filepath.SkipDir
			}
			return nil
		}

		ignored := ignoredBy(ignore, path)
		if ignored {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   relative,
				Reason: SkipIgnored,
				Detail: "ignored by .gitignore",
			})
			return nil
		}
		if ignoredBy(istokIgnore, path) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   relative,
				Reason: SkipIgnored,
				Detail: "ignored by .istokignore",
			})
			return nil
		}

		file, reason, detail, keep, err := filterCandidate(path, root, maxSize)
		if err != nil {
			return err
		}
		if !keep {
			diagnostics = append(diagnostics, Diagnostic{
				Path:   relative,
				Reason: reason,
				Detail: detail,
			})
			return nil
		}

		file.Path = relative
		file.AbsPath = path
		files = append(files, file)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Reason < diagnostics[j].Reason
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return Result{Files: files, Diagnostics: diagnostics}, nil
}
