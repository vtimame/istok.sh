package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/vtimame/istok.sh/internal/updater"
)

func TestRestoreBinaryStagesTargetBeforePromotingOld(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "istok")
	old := filepath.Join(dir, "old")
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBinary(target, old); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old" {
		t.Fatalf("target = %q, err = %v", got, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old still exists: %v", err)
	}
}

func TestRestoreBinaryPromotesOldWhenTargetWasRemoved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "istok")
	old := filepath.Join(dir, "old")
	if err := os.WriteFile(old, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := RestoreBinary(target, old); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old" {
		t.Fatalf("target = %q, err = %v", got, err)
	}
}

func TestPromptRejectsAndDoesNotApply(t *testing.T) {
	service := &fakeService{available: true}
	var output strings.Builder
	err := Application{Service: service, Version: "v0.0.1", Input: strings.NewReader("no\n"), Output: &output}.Run(context.Background(), Command{})
	if err != nil {
		t.Fatal(err)
	}
	if service.applied {
		t.Fatal("update unexpectedly applied")
	}
	if !strings.Contains(output.String(), "Update cancelled.") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestCheckDoesNotApply(t *testing.T) {
	service := &fakeService{available: true}
	err := Application{Service: service, Version: "v0.0.1", Input: strings.NewReader(""), Output: &strings.Builder{}}.Run(context.Background(), Command{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if service.applied {
		t.Fatal("check unexpectedly applied")
	}
}

func TestSanitizeStderr(t *testing.T) {
	if got := sanitizeStderr("\x00 migration failed \n"); got != "migration failed" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestCleanupFailedApplyRecoversRetainedOldBinary(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	old := filepath.Join(directory, "old")
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := cleanupFailedApply(errors.New("apply failed"), target, "", old)
	if err == nil || !strings.Contains(err.Error(), "was recovered") {
		t.Fatalf("error = %v, want recovery detail", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "old" {
		t.Fatalf("target = %q, err = %v", got, readErr)
	}
}

type fakeService struct{ available, applied bool }

func (s *fakeService) Check(context.Context, string) (*updater.Update, bool, error) {
	return &updater.Update{Current: "v0.0.1", Target: "v0.0.2"}, s.available, nil
}
func (s *fakeService) Apply(context.Context, *selfupdate.Release, string, string) error {
	s.applied = true
	return nil
}
func (s *fakeService) Executable() (string, error) { return "", errors.New("not expected") }
