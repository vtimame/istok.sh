// Package gitremote identifies the repository a project folder belongs to by
// its git remotes, so devices that cloned the same repository into different
// folders find the same hub project.
//
// Remote URLs are normalized here rather than with a library: the needed rules
// (scheme, user, port-less scp syntax, .git suffix, host case) are a few lines,
// and the available URL-parsing packages either reject scp syntax or pull in a
// whole git implementation.
package gitremote

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Identity names a project folder independently of where it lives: the
// normalized remotes of its repository and the folder's path inside it.
type Identity struct {
	Remotes []string `json:"remotes,omitempty"`
	Subdir  string   `json:"subdir,omitempty"`
}

// Known reports whether the identity has any remote to match by.
func (i Identity) Known() bool {
	return len(i.Remotes) > 0
}

// Matches reports whether two identities name the same folder of the same
// repository: the same path inside it and at least one common remote.
func (i Identity) Matches(other Identity) bool {
	if i.Subdir != other.Subdir {
		return false
	}

	for _, remote := range i.Remotes {
		if slices.Contains(other.Remotes, remote) {
			return true
		}
	}

	return false
}

// Of reads the identity of a folder. A folder outside a git repository, or a
// repository without remotes, has an empty identity and no error: there is
// nothing to match by.
func Of(ctx context.Context, directory string) Identity {
	toplevel, err := git(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		return Identity{}
	}

	urls, err := git(ctx, directory, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil {
		return Identity{}
	}

	var identity Identity
	for _, line := range strings.Split(urls, "\n") {
		_, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if normalized := Normalize(url); normalized != "" && !slices.Contains(identity.Remotes, normalized) {
			identity.Remotes = append(identity.Remotes, normalized)
		}
	}
	slices.Sort(identity.Remotes)

	identity.Subdir = subdir(toplevel, directory)

	return identity
}

// subdir is the folder's path inside the repository, "" at its top.
func subdir(toplevel, directory string) string {
	top, err := filepath.EvalSymlinks(toplevel)
	if err != nil {
		return ""
	}
	folder, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return ""
	}

	relative, err := filepath.Rel(top, folder)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return ""
	}

	return filepath.ToSlash(relative)
}

// Normalize turns the ways one repository is addressed into one form,
// host/path: https://github.com/a/b.git, git@github.com:a/b and
// ssh://git@github.com/a/b all become github.com/a/b. Local paths and file
// URLs stay as they are, minus a trailing .git and slash.
func Normalize(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	host, path := "", url
	switch {
	case strings.HasPrefix(url, "file://"):
		path = strings.TrimPrefix(url, "file://")
	case strings.Contains(url, "://"):
		_, rest, _ := strings.Cut(url, "://")
		host, path, _ = strings.Cut(rest, "/")
	case scpLike(url):
		host, path, _ = strings.Cut(url, ":")
	}

	if host != "" {
		if _, after, ok := strings.Cut(host, "@"); ok {
			host = after
		}
		host = strings.ToLower(strings.TrimSuffix(host, ":"))
		if name, port, ok := strings.Cut(host, ":"); ok && (port == "22" || port == "443" || port == "80") {
			host = name
		}
	}

	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	if host == "" {
		return path
	}

	return host + "/" + strings.TrimPrefix(path, "/")
}

// scpLike reports git's scp syntax, [user@]host:path, which has a colon
// before any slash and is not a Windows drive letter.
func scpLike(url string) bool {
	colon := strings.Index(url, ":")
	if colon <= 1 {
		return false
	}
	slash := strings.Index(url, "/")

	return slash == -1 || colon < slash
}

func git(ctx context.Context, directory string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...).Output()

	return strings.TrimSpace(string(output)), err
}
