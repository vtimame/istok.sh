package mcpserver

// instructions is sent to clients in the initialize result. Clients that
// support server instructions add it to the agent's system prompt, so agents
// learn the Istok workflow without users pasting rules into CLAUDE.md or
// AGENTS.md. It names only core tools, which every profile and tool set
// lists; keep it in sync with the tool set and short, because it costs context
// in every session.
const instructions = `Istok keeps this project's tasks, runs, checks and knowledge outside the chat, so work survives the session and another agent can continue it.

Use it for substantial work: features, fixes, refactoring, migrations, investigations. Skip it for questions, quick reading and tiny edits.

1. Call project_current. If the directory is not registered, tell the user and continue without Istok; call project_init only when the user asks.
2. Find an existing task with task_ready or task_list, then call task_start with its task_id, or with a title to create one. It claims the task or continues your own run, and returns project rules, task context, code hits and the latest handoff. Follow the rules; do not bulk-load other context.
3. Record meaningful steps with task_progress, blockers with task_block and commits with task_commit. Your calls keep the run lease alive.
4. Before you stop or the context is compacted, write task_handoff: what is done, where you stopped, decisions, the next step. Afterwards call task_start again with the same task_id.
5. Record each check with task_evidence (a command, CI or a manual test), or run a command with run_validate.
6. Close with task_done and a summary; pass not_verified_reason if the result cannot be verified. Implementation alone is not completion.

If a lease cannot be resumed, call run_recover with a reason. On revision_conflict, read the task with task_show and retry.

Find knowledge with knowledge_search and read only relevant items with knowledge_read. Locate code with search, then verify it in the source.

This server lists the core tools. Knowledge writing, the code graph, dependencies and run internals need Istok started with --tools full.`
