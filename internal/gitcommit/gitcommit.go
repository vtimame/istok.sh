// Package gitcommit reads git commits in the repositories of a project for
// linking them to tasks.
package gitcommit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vtimame/istok.sh/internal/task"
)

// Inspect reads the commit rev in the repository folder, given relative to
// the project root. An empty repository is the root and an empty rev is HEAD.
func Inspect(ctx context.Context, root, repository, rev string) (task.Commit, error) {
	relative, directory, err := repositoryPath(root, repository)
	if err != nil {
		return task.Commit{}, err
	}

	rev = strings.TrimSpace(rev)
	if rev == "" {
		rev = "HEAD"
	}
	if strings.HasPrefix(rev, "-") {
		return task.Commit{}, task.NewError(task.CodeInvalid, "commit revision must not start with '-'")
	}

	output, err := git(ctx, directory, "show", "-s", "--format=%H%x00%s%x00%cI", rev+"^{commit}", "--")
	if err != nil {
		return task.Commit{}, task.NewError(task.CodeInvalid, "read commit %s in %s: %v", rev, relative, err)
	}

	fields := strings.SplitN(strings.TrimSpace(output), "\x00", 3)
	if len(fields) != 3 {
		return task.Commit{}, fmt.Errorf("unexpected git show output %q", output)
	}
	committedAt, err := time.Parse(time.RFC3339, fields[2])
	if err != nil {
		return task.Commit{}, fmt.Errorf("parse commit time %q: %w", fields[2], err)
	}

	remote, _ := git(ctx, directory, "remote", "get-url", "origin")

	return task.Commit{
		Repository:  relative,
		SHA:         fields[0],
		Subject:     fields[1],
		CommittedAt: committedAt.UTC(),
		Remote:      strings.TrimSpace(remote),
	}, nil
}

// Recent returns the HEAD commits made after since in the project root and
// in the repositories directly inside it, for projects that hold several
// repositories.
func Recent(ctx context.Context, root string, since time.Time) ([]task.Commit, error) {
	repositories := []string{"."}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list project folders: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), ".git")); err == nil {
			repositories = append(repositories, entry.Name())
		}
	}

	var commits []task.Commit
	for _, repository := range repositories {
		commit, err := Inspect(ctx, root, repository, "HEAD")
		if err != nil {
			// A folder that is not a repository or has no commits yet has
			// nothing to link.
			continue
		}
		if commit.CommittedAt.After(since) {
			commits = append(commits, commit)
		}
	}

	return commits, nil
}

// repositoryPath resolves the repository folder and rejects folders outside
// the project root.
func repositoryPath(root, repository string) (string, string, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		repository = "."
	}
	if filepath.IsAbs(repository) {
		return "", "", task.NewError(task.CodeInvalid, "repository must be relative to the project root")
	}

	relative := filepath.Clean(repository)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", task.NewError(task.CodeInvalid, "repository must be inside the project root")
	}

	directory := filepath.Join(root, relative)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve project root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", "", task.NewError(task.CodeInvalid, "repository %s: %v", relative, err)
	}
	inside, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", "", task.NewError(task.CodeInvalid, "repository must be inside the project root")
	}

	return filepath.ToSlash(relative), resolved, nil
}

func git(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)

	var stderr bytes.Buffer
	command.Stderr = &stderr

	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.TrimSpace(stderr.String()) != "" {
			return "", errors.New(strings.TrimSpace(stderr.String()))
		}

		return "", err
	}

	return string(output), nil
}
