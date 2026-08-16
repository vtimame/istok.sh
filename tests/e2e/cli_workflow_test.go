package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var istokBinary string

func TestMain(m *testing.M) {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	buildDir, err := os.MkdirTemp("", "istok-e2e-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	istokBinary = filepath.Join(buildDir, "istok")
	build := exec.Command("go", "build", "-o", istokBinary, "./cmd/istok")
	build.Dir = moduleRoot
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build E2E binary: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()
	if err := os.RemoveAll(buildDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove E2E build directory: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}

	os.Exit(exitCode)
}

func TestCLIWorkflow(t *testing.T) {
	env := newEnvironment(t, true)

	initialized := env.runJSON(t, "init", "--json")
	assertSchemaVersion(t, initialized)
	project := object(t, initialized, "result", "project")
	assertNonEmptyString(t, project, "id")

	created := env.runJSON(t, "task", "create", "--title", "E2E workflow", "--json")
	createdTask := object(t, created, "result", "task")
	if number(t, createdTask, "number") != 1 || stringValue(t, createdTask, "status") != "open" || number(t, createdTask, "revision") != 1 {
		t.Fatalf("created task = %#v", createdTask)
	}

	ready := env.runJSON(t, "task", "ready", "--json")
	assertSchemaVersion(t, ready)
	readyTasks := array(t, ready, "result", "tasks")
	if len(readyTasks) != 1 || number(t, readyTasks[0].(map[string]any), "number") != 1 {
		t.Fatalf("ready tasks = %#v", readyTasks)
	}

	claimed := env.runJSON(t, "task", "claim", "1", "--json")
	run := object(t, claimed, "result")
	runID := stringValue(t, run, "id")
	leaseID := stringValue(t, run, "lease_id")
	if runID == "" || leaseID == "" || stringValue(t, run, "status") != "active" || number(t, run, "revision") != 1 {
		t.Fatalf("claimed run = %#v", run)
	}
	heartbeat := env.runJSON(t, "run", "heartbeat", runID, "--lease", leaseID, "--json")
	if number(t, object(t, heartbeat, "result"), "revision") != 1 {
		t.Fatalf("heartbeat changed semantic revision: %#v", heartbeat)
	}

	earlyRecovery := env.run(t, "run", "recover", runID, "--reason", "lease is still healthy", "--json")
	if earlyRecovery.exitCode != 1 || earlyRecovery.stdout != "" {
		t.Fatalf("early recovery = %+v", earlyRecovery)
	}
	assertSchemaVersion(t, decodeJSON(t, earlyRecovery.stderr))

	recovered := env.runJSON(t, "run", "recover", runID, "--force", "--reason", "E2E takeover", "--json")
	recoveredRun := object(t, recovered, "result")
	leaseID = stringValue(t, recoveredRun, "lease_id")
	if leaseID == "" || leaseID == stringValue(t, run, "lease_id") || number(t, recoveredRun, "revision") != 2 {
		t.Fatalf("recovered run = %#v", recoveredRun)
	}

	progressed := env.runJSON(t, "task", "progress", "1", "--body", "implementation started", "--json")
	if number(t, object(t, progressed, "result", "task"), "revision") != 2 {
		t.Fatalf("progress result = %#v", progressed)
	}

	commented := env.runJSON(t, "task", "comment", "1", "--body", "validation planned", "--json")
	if number(t, object(t, commented, "result", "task"), "revision") != 3 {
		t.Fatalf("comment result = %#v", commented)
	}

	validated := env.runJSON(t,
		"run", "validate", runID,
		"--lease", leaseID,
		"--json",
		"--", "/bin/sh", "-c", "printf stdout-e2e; printf stderr-e2e >&2",
	)
	validationResult := object(t, validated, "result")
	execution := object(t, validationResult, "execution")
	validation := object(t, validationResult, "validation")
	if stringValue(t, execution, "status") != "succeeded" || boolValue(t, execution, "timed_out") {
		t.Fatalf("validation execution = %#v", execution)
	}
	if stringValue(t, validation, "source") != "executed" || stringValue(t, validation, "status") != "passed" {
		t.Fatalf("validation = %#v", validation)
	}
	validationID := stringValue(t, validation, "id")
	artifacts := array(t, validationResult, "artifacts")
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %#v, want stdout and stderr", artifacts)
	}
	artifactKinds := make(map[string]bool, len(artifacts))
	for _, value := range artifacts {
		artifactKinds[stringValue(t, value.(map[string]any), "kind")] = true
	}
	if !artifactKinds["stdout"] || !artifactKinds["stderr"] {
		t.Fatalf("artifact kinds = %#v, want stdout and stderr", artifactKinds)
	}

	shown := env.runJSON(t, "run", "show", runID, "--json")
	showResult := object(t, shown, "result")
	assertArrayNotNull(t, showResult, "executions")
	assertArrayNotNull(t, showResult, "validations")
	assertArrayNotNull(t, showResult, "artifacts")
	if number(t, object(t, showResult, "run"), "revision") != 2 {
		t.Fatalf("managed validation changed run revision: %#v", showResult)
	}
	if len(array(t, showResult, "executions")) != 1 || len(array(t, showResult, "validations")) != 1 || len(array(t, showResult, "artifacts")) != 2 {
		t.Fatalf("run show = %#v", showResult)
	}

	listed := env.runJSON(t, "run", "artifact", "list", runID, "--json")
	listedArtifacts := array(t, listed, "result")
	if len(listedArtifacts) != 2 {
		t.Fatalf("listed artifacts = %#v", listedArtifacts)
	}
	for _, value := range listedArtifacts {
		artifact := value.(map[string]any)
		artifactID := stringValue(t, artifact, "id")
		verified := env.runJSON(t, "run", "artifact", "verify", artifactID, "--run", runID, "--json")
		verification := object(t, verified, "result")
		if !boolValue(t, verification, "verified") || stringValue(t, object(t, verification, "artifact"), "id") != artifactID {
			t.Fatalf("artifact verification = %#v", verification)
		}
	}

	finished := env.runJSON(t,
		"run", "finish", runID,
		"--lease", leaseID,
		"--expected-revision", "2",
		"--status", "succeeded",
		"--summary", "E2E validation passed",
		"--json",
	)
	finishedRun := object(t, finished, "result")
	if stringValue(t, finishedRun, "status") != "succeeded" || number(t, finishedRun, "revision") != 3 {
		t.Fatalf("finished run = %#v", finished)
	}

	completed := env.runJSON(t,
		"task", "done", "1",
		"--expected-revision", "3",
		"--run-id", runID,
		"--validation-id", validationID,
		"--note", "E2E workflow complete",
		"--json",
	)
	completedTask := object(t, completed, "result", "task")
	if stringValue(t, completedTask, "status") != "done" || number(t, completedTask, "revision") != 4 {
		t.Fatalf("completed task = %#v", completedTask)
	}

	final := env.runJSON(t, "task", "show", "1", "--json")
	finalTask := object(t, final, "result", "task", "task")
	if stringValue(t, finalTask, "status") != "done" || number(t, finalTask, "revision") != 4 {
		t.Fatalf("final task = %#v", finalTask)
	}
	assertArrayNotNull(t, object(t, final, "result", "task"), "events")
}

func TestDangerousCommandRequiresOverride(t *testing.T) {
	env := newEnvironment(t, true)
	env.runJSON(t, "init", "--json")
	env.runJSON(t, "task", "create", "--title", "Dangerous command", "--json")
	claimed := env.runJSON(t, "task", "claim", "1", "--json")
	run := object(t, claimed, "result")
	runID, leaseID := stringValue(t, run, "id"), stringValue(t, run, "lease_id")

	failed := env.run(t, "run", "exec", runID, "--lease", leaseID, "--json", "--", "rm", "-rf", "victim")
	if failed.exitCode != 1 || failed.stdout != "" {
		t.Fatalf("dangerous rejection = %+v", failed)
	}
	errorResponse := decodeJSON(t, failed.stderr)
	assertSchemaVersion(t, errorResponse)
	assertNonEmptyString(t, object(t, errorResponse, "error"), "code")

	allowed := env.runJSON(t,
		"run", "exec", runID,
		"--lease", leaseID,
		"--allow-dangerous",
		"--override-reason", "E2E isolated temporary project",
		"--json",
		"--", "rm", "-rf", "victim",
	)
	if stringValue(t, object(t, allowed, "result", "execution"), "status") != "succeeded" {
		t.Fatalf("allowed dangerous command = %#v", allowed)
	}
}

func TestInitUsesXDGDataPathWithoutDatabaseOverride(t *testing.T) {
	env := newEnvironment(t, false)
	env.runJSON(t, "init", "--json")

	database := filepath.Join(env.dataHome, "istok", "istok.db")
	if info, err := os.Stat(database); err != nil || info.IsDir() {
		t.Fatalf("default XDG database %s: %v", database, err)
	}
	if matches, err := filepath.Glob(filepath.Join(env.root, ".istok*")); err != nil || len(matches) != 0 {
		t.Fatalf("project root has unexpected local state: %v, %v", matches, err)
	}
}

type testEnvironment struct {
	root     string
	dataHome string
	env      []string
}

func newEnvironment(t *testing.T, withDatabase bool) testEnvironment {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, "project")
	home := filepath.Join(base, "home")
	dataHome := filepath.Join(base, "xdg-data")
	tmpDir := filepath.Join(base, "tmp")
	for _, path := range []string{root, home, dataHome, tmpDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
	}

	env := []string{
		"HOME=" + home,
		"XDG_DATA_HOME=" + dataHome,
		"TMPDIR=" + tmpDir,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
	}
	if withDatabase {
		env = append(env, "ISTOK_DATABASE="+filepath.Join(base, "istok.db"))
	}

	return testEnvironment{root: root, dataHome: dataHome, env: env}
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func (env testEnvironment) run(t *testing.T, args ...string) commandResult {
	t.Helper()

	command := exec.Command(istokBinary, args...)
	command.Dir = env.root
	command.Env = env.env

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("run %q: %v", args, err)
	}
	result.exitCode = exitError.ExitCode()

	return result
}

func (env testEnvironment) runJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()

	result := env.run(t, args...)
	if result.exitCode != 0 || result.stderr != "" {
		t.Fatalf("run %q: exit=%d stdout=%q stderr=%q", args, result.exitCode, result.stdout, result.stderr)
	}

	response := decodeJSON(t, result.stdout)
	assertSchemaVersion(t, response)

	return response
}

func decodeJSON(t *testing.T, value string) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(value))
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode JSON %q: %v", value, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("JSON output is not a single document %q: %v", value, err)
	}

	return response
}

func assertSchemaVersion(t *testing.T, response map[string]any) {
	t.Helper()
	if stringValue(t, response, "schema_version") != "1" {
		t.Fatalf("schema_version = %#v, want 1", response["schema_version"])
	}
}

func object(t *testing.T, value map[string]any, path ...string) map[string]any {
	t.Helper()

	var current any = value
	for _, key := range path {
		objectValue, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s is %T, want object", strings.Join(path, "."), current)
		}
		current, ok = objectValue[key]
		if !ok || current == nil {
			t.Fatalf("missing object %s", strings.Join(path, "."))
		}
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
		objectValue, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s is %T, want object", strings.Join(path, "."), current)
		}
		current, ok = objectValue[key]
		if !ok || current == nil {
			t.Fatalf("missing array %s", strings.Join(path, "."))
		}
	}

	result, ok := current.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", strings.Join(path, "."), current)
	}

	return result
}

func assertArrayNotNull(t *testing.T, value map[string]any, key string) {
	t.Helper()
	if _, ok := value[key].([]any); !ok {
		t.Fatalf("%s = %#v, want non-null array", key, value[key])
	}
}

func stringValue(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	result, ok := value[key].(string)
	if !ok {
		t.Fatalf("%s = %#v, want string", key, value[key])
	}

	return result
}

func assertNonEmptyString(t *testing.T, value map[string]any, key string) {
	t.Helper()
	if stringValue(t, value, key) == "" {
		t.Fatalf("%s is empty", key)
	}
}

func number(t *testing.T, value map[string]any, key string) int64 {
	t.Helper()
	result, ok := value[key].(float64)
	if !ok {
		t.Fatalf("%s = %#v, want number", key, value[key])
	}

	return int64(result)
}

func boolValue(t *testing.T, value map[string]any, key string) bool {
	t.Helper()
	result, ok := value[key].(bool)
	if !ok {
		t.Fatalf("%s = %#v, want bool", key, value[key])
	}

	return result
}

func findModuleRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("could not find go.mod above %s", directory)
		}
		directory = parent
	}
}
