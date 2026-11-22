// Package worktree fingerprints the content of a Git working tree, so a
// validation can be tied to the code it checked.
//
// A fingerprint is a commit and a digest of every path whose working-tree
// content differs from that commit: tracked changes, staged or not, and
// untracked files that .gitignore does not exclude. Computing the digest again
// against the same commit later tells whether the content changed. Committing
// the checked changes does not: the content stays the same, only where Git
// keeps it moves. Nothing is written to the repository.
package worktree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotRepository means the directory is not inside a Git working tree, or
// Git is not installed.
var ErrNotRepository = errors.New("not a git working tree")

// State is the fingerprint of a working tree.
type State struct {
	Commit string
	Digest string
}

// Capture fingerprints the working tree under root against its HEAD commit,
// or against the empty tree in a repository without commits.
func Capture(ctx context.Context, root string) (State, error) {
	if err := requireRepository(ctx, root); err != nil {
		return State{}, err
	}

	commit, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		commit, err = git(ctx, root, "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return State{}, fmt.Errorf("hash the empty tree: %w", err)
		}
	}
	commit = strings.TrimSpace(commit)

	digest, err := Digest(ctx, root, commit)
	if err != nil {
		return State{}, err
	}

	return State{Commit: commit, Digest: digest}, nil
}

// Digest fingerprints the working tree under root against the given commit or
// tree. Only paths under root count, so a project in a subdirectory of a
// repository ignores changes elsewhere in it.
func Digest(ctx context.Context, root, commit string) (string, error) {
	if err := requireRepository(ctx, root); err != nil {
		return "", err
	}

	changed, err := git(ctx, root, "diff", "--name-only", "-z", "--no-renames", "--relative", commit, "--")
	if err != nil {
		return "", fmt.Errorf("compare the working tree with %s: %w", short(commit), err)
	}
	untracked, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w", err)
	}

	paths := uniquePaths(changed, untracked)

	hash := sha256.New()
	fmt.Fprintf(hash, "istok-worktree-v1\n%s\n", commit)
	for _, path := range paths {
		content, err := describe(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}

		fmt.Fprintf(hash, "%s\x00%s\n", path, content)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func requireRepository(ctx context.Context, root string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrNotRepository
	}

	inside, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return ErrNotRepository
	}

	return nil
}

// describe names the content of one path: the SHA-256 of a file, the target
// of a symbolic link, or that the path is gone or is a directory (a
// submodule).
func describe(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "deleted", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", fmt.Errorf("read link %s: %w", path, err)
		}

		return "link:" + target, nil
	case info.IsDir():
		return "dir", nil
	case !info.Mode().IsRegular():
		return "other", nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	mode := "file"
	if info.Mode()&0o111 != 0 {
		mode = "exec"
	}

	return mode + ":" + hex.EncodeToString(hash.Sum(nil)), nil
}

func uniquePaths(lists ...string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, list := range lists {
		for _, path := range strings.Split(list, "\x00") {
			if path == "" || seen[path] {
				continue
			}

			seen[path] = true
			paths = append(paths, path)
		}
	}

	sort.Strings(paths)

	return paths
}

// git runs a read-only Git command in dir. GIT_OPTIONAL_LOCKS=0 keeps Git
// from refreshing the index, so fingerprinting never takes the index lock an
// agent's own git command may need.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return "", err
		}

		return "", fmt.Errorf("%w: %s", err, message)
	}

	return stdout.String(), nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}

	return commit
}
