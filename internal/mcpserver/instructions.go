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
2. Look for an existing task with task_ready or task_list before creating one with task_create (you generate the UUIDv7 task_id).
3. task_claim starts a run and returns its lease and bounded context: enabled project rules, task-relevant context and code retrieval hits. Follow the returned rules. Do not bulk-load other context or knowledge.
4. Record meaningful steps with task_progress and blockers with task_block. A lease lasts 15 minutes; call run_heartbeat during long work.
5. Validate with run_validate, which runs the command and records the result. For a check run outside Istok, use execution_start, execution_finish and validation_record with source "attested".
6. Finish with run_finish and a result summary; a succeeded run needs a passed validation. Then call task_complete with the run_id and validation_id. Implementation alone is not completion.

Recovery:
- Arguments named expected_revision or expected_task_revision are compare-and-swap. On revision_conflict, read the current revision with task_show or run_show and retry.
- If a lease expired, call run_recover with a reason before continuing the run.

Knowledge and code:
- Find knowledge with knowledge_catalog or knowledge_search and read only relevant items with knowledge_read.
- Save reusable current truth (decisions, runbooks, investigation results) with knowledge_distill as a draft with provenance; review and promote it separately. Do not copy task or run state into context or knowledge.
- Use search and graph_symbol, graph_neighbors, graph_path to locate code, then verify details in the source before editing.`
