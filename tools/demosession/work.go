package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var validationArgv = []string{"go", "test", "./..."}

// playWork is the main session: the agent turns a request into a task, changes
// the code, proves it with go test through Istok and completes the task.
func playWork(ctx context.Context, agent *mcpAgent, demo paths, text copyText) (part, error) {
	session := part{Agent: "claude", CWD: "~/work/acme-api", Prompt: text.workPrompt}

	// The agent records the request as a task before touching code.
	var created taskResult
	err := agent.call(ctx, "task_create", map[string]any{
		"task_id":             uuid.Must(uuid.NewV7()).String(),
		"title":               text.workTitle,
		"acceptance_criteria": text.workAcceptance,
	}, &created)
	if err != nil {
		return session, err
	}

	session.Steps = append(session.Steps, step{
		Kind:    stepIstok,
		Name:    "task_create",
		Summary: fmt.Sprintf("%q", text.workTitle),
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
	var finished runResult
	err = agent.call(ctx, "run_finish", map[string]any{
		"run_id":            runID,
		"lease_id":          leaseID,
		"expected_revision": claimed.Run.Revision,
		"status":            "succeeded",
		"result_summary":    text.workSummary,
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
		"note":                   text.workNote,
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
		step{Kind: stepMessage, Text: text.workReply(completed.Task.Number)},
	)

	return session, nil
}
