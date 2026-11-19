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

	output, err := git(ctx, directory, "show", "-s", "--format="+commitFormat, rev+"^{commit}", "--")
	if err != nil {
		return task.Commit{}, task.NewError(task.CodeInvalid, "read commit %s in %s: %v", rev, relative, err)
	}

	commit, err := parseCommit(strings.TrimSpace(output))
	if err != nil {
		return task.Commit{}, err
	}

	commit.Repository = relative
	commit.Remote = remoteOf(ctx, directory)

	return commit, nil
}

// commitFormat prints one commit per line: sha, subject and committer time
// separated by NUL bytes.
const commitFormat = "%H%x00%s%x00%cI"

// maxRecentCommits bounds the commits Recent reads from one repository.
const maxRecentCommits = 50

func parseCommit(line string) (task.Commit, error) {
	fields := strings.SplitN(line, "\x00", 3)
	if len(fields) != 3 {
		return task.Commit{}, fmt.Errorf("unexpected git output %q", line)
	}

	committedAt, err := time.Parse(time.RFC3339, fields[2])
	if err != nil {
		return task.Commit{}, fmt.Errorf("parse commit time %q: %w", fields[2], err)
	}

	return task.Commit{SHA: fields[0], Subject: fields[1], CommittedAt: committedAt.UTC()}, nil
}

func remoteOf(ctx context.Context, directory string) string {
	remote, _ := git(ctx, directory, "remote", "get-url", "origin")
	return strings.TrimSpace(remote)
}

// Recent returns the commits on HEAD made after since, oldest first, in the
// project root and in the repositories directly inside it, for projects
// that hold several repositories. A command may make several commits, so
// Recent reads all of them, not only HEAD.
func Recent(ctx context.Context, root string, since time.Time) ([]task.Commit, error) {
	repositories, err := repositories(root)
	if err != nil {
		return nil, err
	}

	var commits []task.Commit
	for _, repository := range repositories {
		values, err := recentIn(ctx, root, repository, since)
		if err != nil {
			// A folder that is not a repository or has no commits yet has
			// nothing to link.
			continue
		}
		commits = append(commits, values...)
	}

	return commits, nil
}

func recentIn(ctx context.Context, root, repository string, since time.Time) ([]task.Commit, error) {
	relative, directory, err := repositoryPath(root, repository)
	if err != nil {
		return nil, err
	}

	output, err := git(ctx, directory, "log",
		fmt.Sprintf("-n%d", maxRecentCommits),
		fmt.Sprintf("--since=@%d", since.Unix()),
		"--format="+commitFormat,
		"HEAD", "--",
	)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	remote := ""
	commits := make([]task.Commit, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}

		commit, err := parseCommit(lines[i])
		if err != nil {
			return nil, err
		}
		if !commit.CommittedAt.After(since) {
			continue
		}

		if remote == "" {
			remote = remoteOf(ctx, directory)
		}
		commit.Repository = relative
		commit.Remote = remote
		commits = append(commits, commit)
	}

	return commits, nil
}

// Lost reports the linked commits that no branch, tag or remote branch holds
// any more, usually because the history was rewritten after linking. Commits
// of repositories that cannot be read are left out: their state is unknown.
func Lost(ctx context.Context, root string, commits []task.Commit) map[string]bool {
	lost := map[string]bool{}

	for _, commit := range commits {
		_, directory, err := repositoryPath(root, commit.Repository)
		if err != nil {
			continue
		}

		if _, err := git(ctx, directory, "cat-file", "-e", commit.SHA+"^{commit}"); err != nil {
			lost[commit.SHA] = true
			continue
		}

		refs, err := git(ctx, directory, "for-each-ref", "--count=1", "--contains", commit.SHA, "--format=%(refname)")
		if err == nil && strings.TrimSpace(refs) == "" {
			lost[commit.SHA] = true
		}
	}

	return lost
}

// repositories lists the project root and the folders directly inside it
// that are git repositories.
func repositories(root string) ([]string, error) {
	values := []string{"."}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list project folders: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), ".git")); err == nil {
			values = append(values, entry.Name())
		}
	}

	return values, nil
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
