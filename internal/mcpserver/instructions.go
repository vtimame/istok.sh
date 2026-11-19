package mcpserver

// instructions is sent to clients in the initialize result. Clients that
// support server instructions add it to the agent's system prompt, so agents
// learn the Istok workflow without users pasting rules into CLAUDE.md or
// AGENTS.md. It names only tools that every profile exposes; keep it in sync
// with the tool set and short, because it costs context in every session.
const instructions = `Istok keeps this project's tasks, runs, validation evidence, rules and knowledge outside the chat, so work survives the session and another agent can continue it.

Use Istok for substantial work: features, bug fixes, refactoring, migrations, investigations. Skip it for questions, explanations, quick reading and tiny edits.

Workflow:
1. Call project_current. If the directory is not registered, tell the user and continue without Istok; call project_init only when the user asks.
2. Look for an existing task with task_ready or task_list. Then call task_start with its task_id, or with a title to create a new task. It claims the task, or continues your own active run of it, and returns the run, bounded context (enabled project rules, task-relevant context, code retrieval hits) and the handoff: the latest handoff note, recent notes and earlier runs. Follow the returned rules. Do not bulk-load other context or knowledge.
3. Record meaningful steps with task_progress and blockers with task_block. Your Istok calls keep the run lease alive; after a pause the next call resumes it.
4. Before you stop, and when the context is about to be compacted, write task_handoff: what is done, where you stopped, decisions, the next step. After a restart or a compaction call task_start again with the same task_id.
5. Record each check with task_evidence: a command, a CI pipeline or a manual test, passed or failed. run_validate also runs a command for you and records the result.
6. Close with task_done and a summary. It uses the passed validation of your run; if the result cannot be verified, pass not_verified_reason instead. Implementation alone is not completion.

Recovery:
- Arguments named expected_revision or expected_task_revision are compare-and-swap. On revision_conflict, read the current revision with task_show or run_show and retry.
- If a lease cannot be resumed, for example because a command was still running, call run_recover with a reason.

Knowledge and code:
- Find knowledge with knowledge_catalog or knowledge_search and read only relevant items with knowledge_read.
- Save reusable current truth (decisions, runbooks, investigation results) with knowledge_distill as a draft with provenance; review and promote it separately. Do not copy task or run state into context or knowledge.
- Use search and graph_symbol, graph_neighbors, graph_path to locate code, then verify details in the source before editing.`
