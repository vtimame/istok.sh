package discovery

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverGitTrackedUntrackedIgnored(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o600); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked"), 0o600); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	runGit(t, root, "init")
	runGit(t, root, "add", "tracked.txt", ".gitignore")
	runGit(t, root, "commit", "-m", "base")

	result, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	found := map[string]bool{}
	for _, item := range result.Files {
		found[item.Path] = true
	}
	if !found["tracked.txt"] {
		t.Error("tracked file should be discovered")
	}
	if !found["untracked.txt"] {
		t.Error("untracked file should be discovered")
	}
	if found["ignored.txt"] {
		t.Error("ignored file should not be discovered")
	}

	if !hasDiagnostic(result.Diagnostics, "ignored.txt", SkipIgnored) {
		t.Error("missing ignored skip diagnostic")
	}
}

func TestDiscoverFallbackNestedGitignoreAndAdditiveIstokIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".gitignore"), []byte("secret.txt\n"), 0o600); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".istokignore"), []byte("nested/private.txt\n!nested/secret.txt\n"), 0o600); err != nil {
		t.Fatalf("write .istokignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "public.txt"), []byte("public"), 0o600); err != nil {
		t.Fatalf("write public file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "private.txt"), []byte("private"), 0o600); err != nil {
		t.Fatalf("write private file: %v", err)
	}

	result, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	found := map[string]bool{}
	for _, item := range result.Files {
		found[item.Path] = true
	}
	if found["nested/secret.txt"] {
		t.Errorf("istokignore negation must not restore a file ignored by .gitignore: files=%v diagnostics=%v", result.Files, result.Diagnostics)
	}
	if !found["nested/public.txt"] {
		t.Error("public file should be discovered")
	}
	if found["nested/private.txt"] {
		t.Error("istokignore should add an exclusion")
	}
	if !hasDiagnostic(result.Diagnostics, "nested/private.txt", SkipIgnored) {
		t.Error("missing .istokignore diagnostic")
	}
}

func TestDiscoverGitProjectRootBoundaryAndAdditivePolicies(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}

	repo := t.TempDir()
	root := filepath.Join(repo, "project")
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o700); err != nil {
		t.Fatalf("MkdirAll(build): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "kept.go"), []byte("package build"), 0o600); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o700); err != nil {
		t.Fatalf("MkdirAll(vendor): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "legacy.go"), []byte("package vendor"), 0o600); err != nil {
		t.Fatalf("write vendor file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatalf("MkdirAll(src): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "kept.go"), []byte("package main"), 0o600); err != nil {
		t.Fatalf("write kept.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".istokignore"), []byte("src/main.go\n!build/kept.go\n"), 0o600); err != nil {
		t.Fatalf("write .istokignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "outside.go"), []byte("package outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	runGit(t, repo, "init")
	runGit(t, repo, "add", filepath.Join("project", "build", "kept.go"), filepath.Join("project", "vendor", "legacy.go"), filepath.Join("project", "src", "main.go"), filepath.Join("project", "src", "kept.go"), filepath.Join("project", ".istokignore"))
	runGit(t, repo, "add", "outside.go")
	runGit(t, repo, "commit", "-m", "base")

	result, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	files := map[string]bool{}
	for _, item := range result.Files {
		files[item.Path] = true
	}
	if files["build/kept.go"] {
		t.Error("istokignore negation must not override built-in exclusions")
	}
	if files["vendor/legacy.go"] {
		t.Error("builtin skip should filter vendor directory in git mode")
	}
	if files["src/main.go"] {
		t.Error("istokignore should exclude tracked files in git mode")
	}
	if !files["src/kept.go"] {
		t.Error("non-excluded project file should be discovered")
	}
	if files["outside.go"] {
		t.Error("project root should be limited to provided root")
	}
}

func TestDiscoverSecretBinaryOversizedSymlinkAndUtf8(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "unicode.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write valid utf8: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid.txt"), []byte{0xff, 0x61}, 0o600); err != nil {
		t.Fatalf("write invalid utf8: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), bytes.Repeat([]byte("a"), MaxFileSizeBytes+1), 0o600); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	outside := t.TempDir()
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("MkdirAll outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(root, "escape.txt")); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not supported") {
			t.Skip("symlinks unavailable")
		}
		t.Fatalf("create symlink: %v", err)
	}

	result, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	paths := make([]string, 0, len(result.Files))
	for _, item := range result.Files {
		paths = append(paths, item.Path)
	}
	for _, path := range []string{
		"unicode.txt",
	} {
		if !contains(paths, path) {
			t.Fatalf("want discovered path %q", path)
		}
	}
	if hasDiagnostic(result.Diagnostics, ".env", SkipSecret) {
		// ok
	} else {
		t.Error("missing .env skip")
	}
	if !hasDiagnostic(result.Diagnostics, "binary.bin", SkipBinary) {
		t.Error("missing binary skip")
	}
	if !hasDiagnostic(result.Diagnostics, "invalid.txt", SkipInvalidUTF8) {
		t.Error("missing invalid utf8 skip")
	}
	if !hasDiagnostic(result.Diagnostics, "big.txt", SkipTooLarge) {
		t.Error("missing oversized skip")
	}
	if !hasDiagnostic(result.Diagnostics, "escape.txt", SkipSymlinkEscape) {
		t.Error("missing symlink escape skip")
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=istok",
		"GIT_AUTHOR_EMAIL=istok@example.com",
		"GIT_COMMITTER_NAME=istok",
		"GIT_COMMITTER_EMAIL=istok@example.com",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(output))
	}
}

func hasDiagnostic(diagnostics []Diagnostic, path string, reason SkipReason) bool {
	for _, item := range diagnostics {
		if item.Path == path && item.Reason == reason {
			return true
		}
	}
	return false
}

func contains(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}
