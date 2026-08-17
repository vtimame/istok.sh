//go:build hardening

package hardening

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const hardeningTimeout = 2 * time.Minute

type environment struct {
	root      string
	database  string
	indexRoot string
	env       []string
}

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func newEnvironment(t *testing.T) environment {
	t.Helper()

	binary := os.Getenv("ISTOK_HARDENING_BINARY")
	if binary == "" {
		t.Fatal("ISTOK_HARDENING_BINARY is required")
	}
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		t.Fatalf("ISTOK_HARDENING_BINARY %q is not an executable file: %v", binary, err)
	}

	base := t.TempDir()
	root := filepath.Join(base, "project")
	indexRoot := filepath.Join(base, "indexes")
	for _, path := range []string{root, indexRoot, filepath.Join(base, "home"), filepath.Join(base, "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
	}

	return environment{
		root: root, database: filepath.Join(base, "istok.db"), indexRoot: indexRoot,
		env: []string{
			"HOME=" + filepath.Join(base, "home"),
			"TMPDIR=" + filepath.Join(base, "tmp"),
			"PATH=" + os.Getenv("PATH"),
			"GIT_CONFIG_NOSYSTEM=1",
			"ISTOK_DATABASE=" + filepath.Join(base, "istok.db"),
			"ISTOK_INDEX_ROOT=" + indexRoot,
		},
	}
}

func (e environment) command(ctx context.Context, args ...string) commandResult {
	command := exec.CommandContext(ctx, os.Getenv("ISTOK_HARDENING_BINARY"), args...)
	command.Dir = e.root
	command.Env = e.env

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func (e environment) runJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hardeningTimeout)
	defer cancel()
	result := e.command(ctx, args...)
	if result.err != nil {
		t.Fatalf("istok %q: %v\nstdout:\n%s\nstderr:\n%s", args, result.err, result.stdout, result.stderr)
	}
	if result.stderr != "" {
		t.Fatalf("istok %q wrote stderr:\n%s", args, result.stderr)
	}
	return decodeJSON(t, result.stdout)
}

func (e environment) runBoundedJSON(t *testing.T, maximum int, args ...string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hardeningTimeout)
	defer cancel()
	result := e.command(ctx, args...)
	if result.err != nil {
		t.Fatalf("istok %q: %v\nstdout:\n%s\nstderr:\n%s", args, result.err, result.stdout, result.stderr)
	}
	if result.stderr != "" || len(result.stdout) > maximum {
		t.Fatalf("istok %q output is not bounded: stdout=%d stderr=%d maximum=%d", args, len(result.stdout), len(result.stderr), maximum)
	}
	return decodeJSON(t, result.stdout)
}

func decodeJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(text))
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode JSON %q: %v", text, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON output is not one document %q: %v", text, err)
	}
	if stringValue(t, value, "schema_version") != "1" {
		t.Fatalf("schema_version = %#v, want 1", value["schema_version"])
	}
	return value
}

func object(t *testing.T, value map[string]any, path ...string) map[string]any {
	t.Helper()
	var current any = value
	for _, key := range path {
		items, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s is %T, want object", strings.Join(path, "."), current)
		}
		current = items[key]
	}
	result, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object", strings.Join(path, "."), current)
	}
	return result
}

func array(t *testing.T, value map[string]any, path ...string) []any {
	t.Helper()
	var current any = value
	for _, key := range path {
		current = current.(map[string]any)[key]
	}
	result, ok := current.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", strings.Join(path, "."), current)
	}
	return result
}

func stringValue(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	result, ok := value[key].(string)
	if !ok {
		t.Fatalf("%s = %#v, want string", key, value[key])
	}
	return result
}

func numberValue(t *testing.T, value map[string]any, key string) int64 {
	t.Helper()
	result, ok := value[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v, want number", key, value[key])
	}
	return int64(result)
}

func corpusSize(t *testing.T) int {
	t.Helper()
	value := 2500
	if text := os.Getenv("ISTOK_HARDENING_FILES"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil {
			t.Fatalf("ISTOK_HARDENING_FILES=%q: %v", text, err)
		}
		value = parsed
	}
	if value < 200 {
		t.Fatalf("ISTOK_HARDENING_FILES=%d, want at least 200", value)
	}
	return value
}

func writeCorpus(t *testing.T, root string, files int) {
	t.Helper()
	for i := 0; i < files; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg/%02d", i%31))
		var name, body string
		switch i % 5 {
		case 0:
			name, body = fmt.Sprintf("item_%04d.go", i), fmt.Sprintf("package fixture\n\nfunc HardeningSymbol%04d() string { return \"needle-%04d\" }\n", i, i)
		case 1:
			name, body = fmt.Sprintf("item_%04d.ts", i), fmt.Sprintf("export function hardeningSymbol%04d(): string { return 'needle-%04d' }\n", i, i)
		case 2:
			name, body = fmt.Sprintf("item_%04d.md", i), fmt.Sprintf("# Fixture %04d\n\nDeterministic lexical token needle-%04d.\n", i, i)
		case 3:
			dir = filepath.Join(root, "generated")
			name, body = fmt.Sprintf("item_%04d.gen.go", i), "// Code generated by hardening test. DO NOT EDIT.\npackage generated\n"
		default:
			dir = filepath.Join(root, "ignored")
			name, body = fmt.Sprintf("item_%04d.bin", i), string([]byte{0, byte(i), 0xff, 1})
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create corpus directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write corpus file: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\ngenerated/\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
}

func status(t *testing.T, e environment) map[string]any {
	return object(t, e.runJSON(t, "index", "status", "--json"), "result", "status")
}

func projectID(t *testing.T, e environment) string {
	return stringValue(t, object(t, e.runJSON(t, "project", "show", "--json"), "result"), "id")
}

func currentEpoch(t *testing.T, e environment) string {
	return stringValue(t, status(t, e), "epoch_id")
}

func currentEpochOnDisk(t *testing.T, e environment, projectID string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(e.indexRoot, projectID, "CURRENT"))
	if err != nil {
		t.Fatalf("read CURRENT: %v", err)
	}
	return strings.TrimSpace(string(contents))
}

func generationDir(e environment, projectID, epoch string) string {
	return filepath.Join(e.indexRoot, projectID, "generations", epoch)
}

func generationNames(t *testing.T, e environment, projectID string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(e.indexRoot, projectID, "generations"))
	if err != nil {
		t.Fatalf("read generations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func directorySize(t *testing.T, root string) int64 {
	t.Helper()
	var size int64
	if err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatalf("size %s: %v", root, err)
	}
	return size
}
