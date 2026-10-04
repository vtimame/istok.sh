package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// playMemory shows project memory across agents: the user asks Claude Code to
// remember a rule, and a later Codex session receives it when it claims a task.
func playMemory(ctx context.Context, binary string, demo paths, text copyText) (transcript, error) {
	var session transcript

	remembered, err := playRemember(ctx, binary, demo, text)
	if err != nil {
		return session, err
	}

	followed, err := playFollow(ctx, binary, demo, text)
	if err != nil {
		return session, err
	}

	session.Parts = []part{remembered, followed}

	return session, nil
}

func playRemember(ctx context.Context, binary string, demo paths, text copyText) (part, error) {
	session := part{Agent: "claude", CWD: "~/work/acme-api", Prompt: text.memoryPrompt}

	agent, err := connect(ctx, binary, demo, claude)
	if err != nil {
		return session, err
	}
	defer agent.close()

	// An instruction delivered always reaches every run in the project.
	var added contextResult
	err = agent.call(ctx, "context_add", map[string]any{
		"title":    text.memoryTitle,
		"body":     text.memoryBody,
		"kind":     "instruction",
		"source":   "user",
		"delivery": "always",
	}, &added)
	if err != nil {
		return session, err
	}

	session.Steps = []step{
		{
			Kind:    stepIstok,
			Name:    "context_add",
			Summary: fmt.Sprintf("%q", added.Context.Title),
			Output: []line{
				{Text: fmt.Sprintf("saved · %s · delivery %s", added.Context.Kind, added.Context.Delivery), Tone: toneSuccess},
			},
		},
		{Kind: stepMessage, Text: text.memoryReply},
	}

	return session, nil
}

func playFollow(ctx context.Context, binary string, demo paths, text copyText) (part, error) {
	session := part{Agent: "codex", CWD: "~/work/acme-api", Prompt: text.followPrompt}

	agent, err := connect(ctx, binary, demo, codex)
	if err != nil {
		return session, err
	}
	defer agent.close()

	var created taskResult
	err = agent.call(ctx, "task_create", map[string]any{
		"task_id": uuid.Must(uuid.NewV7()).String(),
		"title":   text.followTitle,
	}, &created)
	if err != nil {
		return session, err
	}

	var claimed claimResult
	err = agent.call(ctx, "task_claim", map[string]any{"task_id": created.Task.ID, "base_branch": "main"}, &claimed)
	if err != nil {
		return session, err
	}

	// The claim's snapshot lists the project rules; the one saved a moment ago
	// by another agent is highlighted.
	output := []line{
		{Text: "run started"},
		{Text: "context: " + count(len(claimed.Snapshot.Records), "project rule"), Tone: toneMuted},
	}
	found := false
	for _, record := range claimed.Snapshot.Records {
		tone := toneMuted
		if record.Title == text.memoryTitle {
			tone, found = toneSuccess, true
		}
		output = append(output, line{Text: "· " + record.Title, Tone: tone})
	}
	if !found {
		return session, fmt.Errorf("the saved rule %q is missing from the claim's context", text.memoryTitle)
	}

	session.Steps = []step{
		{
			Kind:    stepIstok,
			Name:    "task_create",
			Summary: fmt.Sprintf("%q", text.followTitle),
			Output:  []line{{Text: fmt.Sprintf("task #%d · %s", created.Task.Number, created.Task.Status)}},
		},
		{
			Kind:    stepIstok,
			Name:    "task_claim",
			Summary: fmt.Sprintf("#%d", created.Task.Number),
			Output:  output,
		},
		{Kind: stepMessage, Text: text.followReply},
	}

	return session, nil
}
