package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	handoffapp "github.com/vtimame/istok.sh/internal/application/handoff"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestResumePrintsNothingOutsideAProject(t *testing.T) {
	database := filepath.Join(t.TempDir(), "resume.db")

	var output, errorOutput bytes.Buffer
	err := ExecuteAt(context.Background(), []string{"resume", "--database", database}, strings.NewReader(""), &output, &errorOutput, t.TempDir())
	if err != nil {
		t.Fatalf("resume outside a project: %v, stderr %s", err, errorOutput.String())
	}
	if strings.TrimSpace(output.String()) != "" {
		t.Fatalf("resume outside a project printed %q", output.String())
	}
}

func TestRenderResumeReportListsTheHandoff(t *testing.T) {
	now := time.Now()
	agent := run.ActorSnapshot{ID: "agent", Kind: "agent", Name: "Claude"}

	report := resumeReport{
		Registered: true,
		Project:    &project.Project{Name: "istok"},
		Tasks: []handoffapp.Brief{{
			Task:      task.Task{ID: "task-id", Number: 7, Title: "Resume work"},
			Handoff:   &task.Event{Type: task.EventHandoff, Body: "done: schema\nnext: CLI", Actor: task.ActorSnapshot{Name: "Claude"}, CreatedAt: now},
			Notes:     []task.Event{{Type: task.EventProgress, Body: "wired\nthe flag", CreatedAt: now}},
			ActiveRun: &handoffapp.RunBrief{ID: "run-id", LeaseOwner: agent, ExpiresAt: now.Add(-time.Minute), LeaseExpired: true},
			Runs:      []handoffapp.RunBrief{{ID: "old-run", Status: run.StatusFailed, ResultSummary: "tests failed"}},
		}},
	}

	rendered := renderResumeReport(report)
	for _, want := range []string{
		"Task #7 Resume work",
		"task_id: task-id",
		"run: run-id by Claude, lease expired at",
		"    done: schema\n    next: CLI",
		"progress: wired the flag",
		"earlier run old-run: failed: tests failed",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("resume output lacks %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "\x1b[") {
		t.Errorf("resume output has terminal styling:\n%s", rendered)
	}

	empty := renderResumeReport(resumeReport{Registered: true, Project: &project.Project{Name: "istok"}})
	if empty != `Istok project "istok": no interrupted work.` {
		t.Errorf("empty resume = %q", empty)
	}
}
