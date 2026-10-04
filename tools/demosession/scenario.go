package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	prompt    = "Reject orders with a zero or negative amount."
	taskTitle = "Reject orders with a non-positive amount"
)

var validationArgv = []string{"go", "test", "./..."}

// Only the fields the transcript shows are decoded from the tool results.

type taskResult struct {
	Task struct {
		ID       string `json:"id"`
		Number   int64  `json:"number"`
		Revision int64  `json:"revision"`
		Status   string `json:"status"`
	} `json:"task"`
}

type claimResult struct {
	Run struct {
		ID       string `json:"id"`
		LeaseID  string `json:"lease_id"`
		Revision int64  `json:"revision"`
	} `json:"run"`
	Snapshot struct {
		Records   []struct{} `json:"records"`
		Retrieval []struct {
			Path string `json:"path"`
		} `json:"retrieval"`
		Knowledge []struct{} `json:"knowledge_catalog"`
	} `json:"snapshot"`
}

type executionResult struct {
	Execution struct {
		ExitCode   *int   `json:"exit_code"`
		DurationMS *int64 `json:"duration_ms"`
	} `json:"execution"`
	Artifacts []struct {
		Kind         string `json:"kind"`
		RelativePath string `json:"relative_path"`
	} `json:"artifacts"`
	Validation *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"validation"`
}

type runResult struct {
	Run struct {
		Status string `json:"status"`
	} `json:"run"`
}

func playSession(ctx context.Context, agent *mcpAgent, demo paths) (transcript, error) {
	session := transcript{CWD: "~/work/acme-api", Prompt: prompt}

	// The agent records the request as a task before touching code.
	var created taskResult
	err := agent.call(ctx, "task_create", map[string]any{
		"task_id":             uuid.Must(uuid.NewV7()).String(),
		"title":               taskTitle,
		"acceptance_criteria": "CreateOrder rejects zero and negative amounts without charging the customer.",
	}, &created)
	if err != nil {
		return session, err
	}
	session.TaskNumber = created.Task.Number

	session.Steps = append(session.Steps, step{
		Kind:    stepIstok,
		Name:    "task_create",
		Summary: fmt.Sprintf("%q", taskTitle),
		Output:  []line{{Text: fmt.Sprintf("task #%d · %s", created.Task.Number, created.Task.Status)}},
	})

	// Claiming starts a run and hands the agent its context.
	var claimed claimResult
	err = agent.call(ctx, "task_claim", map[string]any{"task_id": created.Task.ID, "base_branch": "main"}, &claimed)
	if err != nil {
		return session, err
	}

	session.Steps = append(session.Steps, step{
		Kind:    stepIstok,
		Name:    "task_claim",
		Summary: fmt.Sprintf("#%d", created.Task.Number),
		Output: []line{
			{Text: "run started"},
			{Text: contextSummary(claimed), Tone: toneMuted},
		},
	})

	runID, leaseID := claimed.Run.ID, claimed.Run.LeaseID

	// The agent's own work on the repository.
	lines, err := readLineCount(demo.repository, serviceFile)
	if err != nil {
		return session, err
	}
	session.Steps = append(session.Steps, step{
		Kind:    stepTool,
		Name:    "Read",
		Summary: serviceFile,
		Output:  []line{{Text: fmt.Sprintf("Read %d lines", lines), Tone: toneMuted}},
	})

	added, err := applyServiceEdits(demo.repository)
	if err != nil {
		return session, err
	}
	session.Steps = append(session.Steps, step{
		Kind:    stepTool,
		Name:    "Update",
		Summary: serviceFile,
		Output:  diffLines(added),
	})

	testLines, err := writeServiceTest(demo.repository)
	if err != nil {
		return session, err
	}
	session.Steps = append(session.Steps, step{
		Kind:    stepTool,
		Name:    "Write",
		Summary: testFile,
		Output:  []line{{Text: fmt.Sprintf("Wrote %d lines", testLines), Tone: toneMuted}},
	})

	// Validation runs through Istok, so the command and its output become evidence.
	var validated executionResult
	err = agent.call(ctx, "run_validate", map[string]any{"run_id": runID, "lease_id": leaseID, "argv": validationArgv}, &validated)
	if err != nil {
		return session, err
	}
	if validated.Validation == nil || validated.Validation.Status != "passed" {
		return session, fmt.Errorf("validation did not pass: %+v", validated)
	}

	output, err := stdoutLines(demo, validated)
	if err != nil {
		return session, err
	}
	session.Steps = append(session.Steps, step{
		Kind:    stepIstok,
		Name:    "run_validate",
		Summary: strings.Join(validationArgv, " "),
		Output: append(output, line{
			Text: fmt.Sprintf("validation %s · exit %d · %s", validated.Validation.Status, *validated.Execution.ExitCode, seconds(*validated.Execution.DurationMS)),
			Tone: toneSuccess,
		}),
	})

	// Finishing the run and completing the task ties the result to the evidence.
	summary := "CreateOrder rejects zero and negative amounts with ErrInvalidAmount before charging; covered by a test."

	var finished runResult
	err = agent.call(ctx, "run_finish", map[string]any{
		"run_id":            runID,
		"lease_id":          leaseID,
		"expected_revision": claimed.Run.Revision,
		"status":            "succeeded",
		"result_summary":    summary,
	}, &finished)
	if err != nil {
		return session, err
	}

	var completed taskResult
	err = agent.call(ctx, "task_complete", map[string]any{
		"task_id":                created.Task.ID,
		"expected_task_revision": created.Task.Revision,
		"run_id":                 runID,
		"validation_id":          validated.Validation.ID,
		"note":                   "Non-positive amounts are rejected before any charge.",
	}, &completed)
	if err != nil {
		return session, err
	}

	session.Steps = append(session.Steps,
		step{
			Kind:    stepIstok,
			Name:    "run_finish",
			Summary: finished.Run.Status,
			Output:  []line{{Text: "run " + finished.Run.Status}},
		},
		step{
			Kind:    stepIstok,
			Name:    "task_complete",
			Summary: fmt.Sprintf("#%d", completed.Task.Number),
			Output:  []line{{Text: fmt.Sprintf("task #%d · %s", completed.Task.Number, completed.Task.Status), Tone: toneSuccess}},
		},
		step{
			Kind: stepMessage,
			Text: fmt.Sprintf(
				"Done. Orders with a zero or negative amount are now rejected before any charge, and go test passes. "+
					"Everything is recorded in Istok as task #%d: open istok ui or run istok task show %d.",
				completed.Task.Number, completed.Task.Number,
			),
		},
	)

	return session, nil
}

func contextSummary(claimed claimResult) string {
	files := map[string]bool{}
	for _, item := range claimed.Snapshot.Retrieval {
		files[item.Path] = true
	}

	return fmt.Sprintf(
		"context: %s, %s from %s, %s",
		count(len(claimed.Snapshot.Records), "project rule"),
		count(len(claimed.Snapshot.Retrieval), "code snippet"),
		count(len(files), "file"),
		count(len(claimed.Snapshot.Knowledge), "knowledge note"),
	)
}

// stdoutLines reads the command output Istok stored as the run's artifact.
func stdoutLines(demo paths, result executionResult) ([]line, error) {
	for _, artifact := range result.Artifacts {
		if artifact.Kind != "stdout" {
			continue
		}

		content, err := os.ReadFile(filepath.Join(demo.artifacts, artifact.RelativePath))
		if err != nil {
			return nil, fmt.Errorf("read validation output: %w", err)
		}

		var lines []line
		for _, text := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
			lines = append(lines, line{Text: strings.ReplaceAll(text, "\t", "  ")})
		}
		return lines, nil
	}

	return nil, fmt.Errorf("validation stored no stdout artifact")
}

func diffLines(added []string) []line {
	var lines []line
	for _, text := range added {
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, line{Text: "+ " + strings.ReplaceAll(text, "\t", "  "), Tone: toneAdded})
	}

	return lines
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

func seconds(milliseconds int64) string {
	return fmt.Sprintf("%.1fs", float64(milliseconds)/1000)
}
