package runworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vtimame/istok.sh/internal/executor"
	runmodel "github.com/vtimame/istok.sh/internal/run"
)

func TestAuthorizeCommandRequiresReasonedDangerousOverride(t *testing.T) {
	input := ExecuteInput{Argv: []string{"rm", "-rf", "build"}}
	if _, err := authorizeCommand(input); runmodel.ErrorCode(err) != runmodel.CodeInvalid {
		t.Fatalf("authorizeCommand() error = %v", err)
	}

	input.AllowDangerous = true
	if _, err := authorizeCommand(input); runmodel.ErrorCode(err) != runmodel.CodeInvalid {
		t.Fatalf("authorizeCommand() without reason error = %v", err)
	}

	input.DangerousReason = "remove generated fixture"
	reason, err := authorizeCommand(input)
	if err != nil || reason != input.DangerousReason {
		t.Fatalf("authorizeCommand() = %q, %v", reason, err)
	}

	safeReason, err := authorizeCommand(ExecuteInput{
		Argv:            []string{"go", "test", "./..."},
		AllowDangerous:  true,
		DangerousReason: "unused",
	})
	if err != nil || safeReason != "" {
		t.Fatalf("safe authorizeCommand() = %q, %v", safeReason, err)
	}
}

func TestScopedWorkingDirectoryRejectsEscapeIncludingSymlink(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	resolved, err := scopedWorkingDirectory(root, nested)
	if err != nil || resolved != nested {
		t.Fatalf("scopedWorkingDirectory() = %q, %v", resolved, err)
	}
	if _, err := scopedWorkingDirectory(root, t.TempDir()); runmodel.ErrorCode(err) != runmodel.CodeInvalid {
		t.Fatalf("outside directory error = %v", err)
	}

	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := scopedWorkingDirectory(root, link); runmodel.ErrorCode(err) != runmodel.CodeInvalid {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestProcessFinishDerivesPersistedOutcome(t *testing.T) {
	execution := runmodel.Execution{ID: mustID(t), Revision: 3}

	succeeded := processFinish(execution, executor.Result{ExitCode: 0}, time.Now(), nil)
	if succeeded.Status != runmodel.ExecutionSucceeded || succeeded.ExitCode == nil || *succeeded.ExitCode != 0 {
		t.Fatalf("succeeded finish = %#v", succeeded)
	}

	timedOut := processFinish(execution, executor.Result{ExitCode: -1, Signal: "killed", TimedOut: true}, time.Now(), nil)
	if timedOut.Status != runmodel.ExecutionFailed || !timedOut.TimedOut {
		t.Fatalf("timed out finish = %#v", timedOut)
	}

	cancelled := processFinish(execution, executor.Result{ExitCode: -1, Cancelled: true}, time.Now(), nil)
	if cancelled.Status != runmodel.ExecutionCancelled {
		t.Fatalf("cancelled finish = %#v", cancelled)
	}
}

func TestExecutionEnvironmentFiltersSecretsAndAddsIdentity(t *testing.T) {
	runID := mustID(t)
	executionID := mustID(t)
	environment := executionEnvironment([]string{"PATH=/bin", "API_TOKEN=secret"}, ExecuteInput{
		RunID:       runID,
		ProjectRoot: "/project",
	}, executionID, "/project/subdir")
	joined := strings.Join(environment, "\n")

	for _, expected := range []string{"PATH=/bin", "ISTOK_RUN_ID=" + runID, "ISTOK_EXECUTION_ID=" + executionID, "ISTOK_PROJECT_ROOT=/project", "PWD=/project/subdir"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("environment does not contain %q: %q", expected, environment)
		}
	}
	if strings.Contains(joined, "API_TOKEN") {
		t.Fatalf("environment leaked filtered secret: %q", environment)
	}
}

func mustID(t *testing.T) string {
	t.Helper()

	id, err := runmodel.NewID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}
