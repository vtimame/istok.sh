# Changelog

All notable changes to Istok are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Istok uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **`istok task show` lists the checks of the task.** A new Checks section
  shows every validation of the task's runs, oldest first: the result, the
  command, its exit code and duration, and whether Istok ran the command
  (`run by Istok`) or the agent only reported it (`reported by Claude`).
  `istok run show` lists the checks of the run the same way, before its
  artifacts. The JSON output is unchanged.

### Fixed

- The Claude Code hook no longer links a commit made before the run started,
  such as the commit HEAD was on when the agent claimed the task.
- `istok task show` no longer repeats the run summary under `completed` when
  `task_done` finished the run and completed the task with the same text.

## [0.5.1] - 2026-11-21

The first release with these changes. The `v0.5.0` tag was never released:
its release checks failed before anything was built.

### Changed

- **The task list shows who works on a task, and where.** `istok task list`
  and `task ready` gain a WORKER column with the agent of the task's active
  run and the hub device it works on (`Claude · macbook`), including runs on
  another device; the JSON and the MCP `task_list` and `task_ready` results
  carry them as `worker` and `worker_device`. A device without a hub shows
  only the agent.
- Rows of the previous schema import into a newer one when the only missing
  columns were added with a default: they take the default instead of
  failing, so a hub keeps working across such an update.
- **`istok mcp` lists the core tools by default.** Agents see the short
  workflow: `project_current`, `project_init`, `task_ready`, `task_list`,
  `task_show`, `task_start`, `task_progress`, `task_block`, `task_handoff`,
  `task_commit`, `task_evidence`, `run_validate`, `task_done`, `run_recover`,
  `context_add`, `context_list`, `context_update`, `knowledge_search`,
  `knowledge_read` and `search`, plus the tools of the
  supervisor and admin profiles. Every tool definition costs agent context,
  so the rest moved behind `istok mcp --tools full` (or
  `ISTOK_MCP_TOOLS=full`). A call to a hidden tool returns
  `tool_unavailable` with that hint. The server instructions are shorter and
  name only core tools.
- `task_progress`, `task_comment` and `task_block` no longer need
  `expected_revision`; without it they write to the current revision.
- Your Istok calls keep the run lease alive, and the next call after a pause
  resumes an expired lease instead of requiring `run_recover`.
- Bundles exported by 0.4.0 do not import into this version: the database
  schema gained the MCP call log and the sync state. Update Istok on both
  devices.

### Removed

- **The web UI.** `istok ui` and `istok ui service` are gone, and the binary
  no longer embeds a frontend; tasks, runs and checks are in
  `istok task list`, `istok task show` and `istok run`. Before you update,
  run `istok ui service uninstall` if you installed the service, since it
  would keep trying to start a command that no longer exists. Agents sync
  through the MCP server and hooks, so nothing replaces the UI service's
  periodic sync. `istok update` no longer restarts a service.

### Added

- **Sync between devices through a hub.** `istok hub serve` runs a hub on any
  `HOST:PORT` (a VPS, a home server) with no domain, certificate authority or
  reverse proxy: the hub and every device have their own key and pin each
  other's fingerprint over mutual TLS. `istok hub device add NAME` prints a
  one-time invitation; on the device `istok sync join CODE` registers it.
  The first device creates the sync key; other devices join with
  `--key` (printed by `istok sync key`). Rows are encrypted on the device, so
  the hub stores tasks, runs, context and knowledge without being able to
  read them. `istok sync enable` publishes the current project, or with
  `--project ID` fetches a hub project into the current folder; `istok sync`
  pulls and pushes changes, merging them with the same rules as
  `istok import`. `istok hub device list|revoke` and `istok hub backup`
  manage the hub. A hub reached differently from different networks, say a
  LAN address at home and a VPN address away, is advertised under several
  addresses separated by commas; devices try them all at once and use the
  first that proves to be the hub. Nothing is written to the repository. The hub limits
  open connections, new connections and requests per address and device,
  bans an address for ten minutes after repeated failed handshakes and
  answers `rate_limited` when a device sends too much. A device that lost
  its database gets its own rows back from the hub. Releases publish the
  hub image `ghcr.io/vtimame/istok-hub` (x86-64 and ARM64, unprivileged,
  data in `/data`); `docker/hub/compose.yml` runs it.
- **A clone finds its hub project.** `istok sync enable` in a fresh clone of
  a repository fetches the hub project of that repository and binds the
  folder, without `--project ID`, whatever the folder is called and whether
  the remote is written as HTTPS or SSH. The remotes are stored encrypted with
  the project name, and `istok sync projects` shows them. Publishing a new
  local project is refused when the hub already has another project of the
  same repository; `--new` publishes it anyway. Projects published by an
  earlier version get their remotes the next time `istok sync enable` runs in
  their folder.
- **The hub hands out task numbers and keeps one device on a task.** A new
  task takes its number from the hub, so tasks created on two devices never
  share a number; a task created while the hub is out of reach gets a local
  number and, if another device took it meanwhile, moves to a free one on the
  next sync with a note in its history. Claiming a task, or continuing your
  run of it on another device, first asks the hub: while another device works
  on the task, the claim is refused with that device's name. Without the hub
  the claim goes through locally as before. Each step waits for the hub at
  most two seconds.
- **Sync runs by itself once a device joined a hub.** The MCP server pulls
  the project before `task_start`, so a handoff written on another device is
  in the brief, and pushes a few seconds after each write and on exit. The
  `SessionStart` and `Stop` hooks sync the current project. Reading tasks
  syncs too: `istok task list`, `task show` and `task ready`, and the MCP
  tools that list or show tasks, runs and rules, first sync the project
  unless it synced within the last 30 seconds, so they show what other
  devices did without `istok sync`. Processes take turns through a lock in
  the sync directory; an unreachable hub delays `task_start` and hooks by at
  most a few seconds and reads by at most two, and reads skip it for 30
  seconds after.
- **Shorter workflow.** `task_start` creates or claims a task, or continues
  your own run of it, and returns the context and the latest handoff in one
  call. `task_evidence` records a check of any kind, including a CI run or a
  manual test without a command. `task_done` finishes the run and completes
  the task in one call, with `not_verified_reason` when the result cannot be
  verified.
- **Handoffs between sessions.** `task_handoff` records what is done, where
  the work stopped and the next step; `task_start` returns it after a
  restart or a compaction. `istok resume` prints the interrupted work of the
  current project. Tell the agent "stop, another agent will continue" and it
  calls `task_handoff` with `release`: its run ends, the task stays open, and
  the other agent's `task_start` takes it at once with the note, instead of
  waiting for the lease to expire.
- **Project rules from the default tools.** `context_add`, `context_list`
  and `context_update` are in the core MCP tool set, so "remember for this
  project: …" works without `--tools full`.
- **Agent hooks.** `istok hooks install claude` (or `codex`) adds hooks that
  print the interrupted work when a session starts and, for Claude Code, keep
  the Istok state in compaction summaries and link new commits to the task
  of the only active run. `--stop` also asks the agent once for a handoff
  when it stops. `istok hooks status` and `uninstall` manage them; other
  settings and hooks are kept, with a backup.
- **Commits on tasks.** `task_commit` links a git commit, read from the
  repository, to a task; `repository` selects a nested repository of the
  project. Linking the same commit twice changes nothing. The Claude Code
  hook links every new commit on HEAD, including several commits made by one
  command and commits rebuilt by `commit-tree`, `reset`, `rebase`,
  `cherry-pick` or `merge`. `istok task show` lists linked commits with their
  subject and marks those no branch holds any more after a history rewrite.
- **Usage report.** `istok mcp` logs tool calls locally, and `istok usage`
  shows which tools agents call, how often they fail and where they drop out
  of the workflow.

## [0.4.0] - 2026-10-05

### Added

- **Move projects between devices.** `istok export` writes projects to a file
  and `istok import` merges it on another device, with `--dry-run` to preview.
  The file carries tasks with their history, runs, validations, shared
  context and knowledge; project folders, command output, code indexes and
  local-only notes stay behind. Changes merge by revision, deletions carry
  over, and a task whose number is taken gets the next free number. The web
  UI has Import and Export buttons.
- **Projects without a folder.** An imported project shows as not on this
  device until you bind its folder in the web UI or with
  `istok project rebind`.

## [0.3.0] - 2026-10-05

### Added

- **Agents learn the workflow from the MCP server.** `istok mcp` now sends
  server instructions: when to use Istok, how to take a task through a run to
  a validated completion, and how to recover from revision conflicts and
  expired leases. Clients that support MCP server instructions pick them up
  without any rules pasted into `CLAUDE.md` or `AGENTS.md`.
- **Run the UI as a service on macOS.** `istok ui service install` sets up a
  launchd agent (`~/Library/LaunchAgents/sh.istok.ui.plist`) that starts at
  login, restarts after a crash and logs to `~/Library/Logs/istok/ui.log`.
  `uninstall`, `start`, `stop`, `restart` and `status` work as on Linux, and
  `istok update` restarts the agent on the new binary.

### Fixed

- `istok task show` wraps long lines at word boundaries and keeps inline code
  in one piece. The history shows the run each event belongs to, the run
  summary or reason as text, and no longer prints raw JSON or completion IDs.
- Task, context and knowledge history keeps events in the order they happened
  when several land within the same millisecond.
- `istok ui service install` no longer says to reinstall the service after an
  update: `istok update` already restarts it.

## [0.2.1] - 2026-10-04

### Fixed

- The task history in the web UI shows a **View run** link for claimed,
  finished and abandoned runs instead of the raw run ID, renders the event text
  as Markdown, and no longer shows the internal completion ID.

## [0.2.0] - 2026-10-04

The first release with a web UI: see what your agents are working on, what
they were given and how they proved their work, without leaving the browser.

### Added

- **Web UI.** `istok ui` serves a local web interface on
  `http://127.0.0.1:7700`, embedded in the binary:
  - projects with open, blocked and done counts, running and stale runs, and
    last activity;
  - task lists with filters and search, task pages with Markdown descriptions,
    history, dependencies and runs;
  - run pages with validations, executions and **What the agent saw**: the
    context records, code snippets with syntax highlighting and knowledge an
    agent received, and how much of the context budget they used;
  - a run feed across all projects, filterable by state (running, stale,
    succeeded, failed) and project name;
  - a `Ctrl/⌘+K` search palette, light and dark themes, and live updates:
    changes made by agents appear within about a second.
- **Actions in the UI.** Abandon a stale run (an active run whose agent stopped
  sending heartbeats) to release its task, and delete a project with
  confirmation. Deleting is reversible with `istok project restore`.
- **Run the UI as a service.** `istok ui service install` sets up a systemd
  user unit on Linux; `uninstall`, `start`, `stop`, `restart` and `status`
  manage it. `istok update` restarts the service on the new binary.
- **Bounded context assembly.** Run snapshots now fit fixed budgets (16 KiB of
  saved context, 32 KiB of code retrieval, 48 KiB in total), with explicit
  delivery modes (`always`, `ranked`, `manual`), lifecycle dates and
  `istok context preview` / `istok context doctor` to see what an agent will
  receive and why.
- **Project knowledge.** Pull-first knowledge stored outside your repository:
  agents distill drafts with provenance, people review and promote them, and
  runs get a short briefing instead of full documents. Available through
  `istok knowledge` and MCP.
- **Nested projects.** A project can now live inside another project's
  directory, for example an app inside a monorepo; the closest project wins.

### Changed

- `istok project list` shows full project paths instead of truncating them.
- The required always-delivered context no longer fails a run claim when it
  exceeds the default item budget: the budget grows up to 64 items while byte
  limits stay strict.

### Fixed

- `go.mod`, `go.work` and `go.sum` are no longer parsed as Go source, which
  left the index of every Go project in a degraded state.
- `istok update` on a local build now explains that the build cannot update
  itself and how to install an official release, instead of printing an
  internal error.

### Security

- The SQLite database file is created with `0600` permissions and its default
  directory with `0700`; unsafe custom database paths are rejected on Unix.

## [0.1.2] - 2026-08-21

### Changed

- `istok task show` renders descriptions, acceptance criteria and history as
  rich Markdown, and keeps long history entries readable.
- `istok context show` renders context as Markdown in the terminal; JSON and
  redirected output stay plain.
- `make install` installs into `~/.local/bin` by default.

## [0.1.1] - 2026-08-18

### Changed

- `make install` builds the binary directly into the install directory instead
  of going through `go install`.

## [0.1.0] - 2026-08-18

The first public release: an offline-first workspace that gives coding agents
durable tasks, runs and evidence.

### Added

- Local projects with tasks, a dependency graph and project-scoped task
  numbers, managed from a human-friendly CLI.
- Runs that record which agent worked on a task, every command it executed and
  the validations that prove the result.
- A local code index with Tree-sitter graphs for Go and TypeScript/JavaScript,
  `istok search` and `istok graph`, and task-aware retrieval that gives an
  agent the relevant code when it claims a task.
- An MCP server (`istok mcp`) so any MCP-capable agent can work with the same
  tasks, runs and context.
- Saved project context and instructions delivered to agents with each run.
- Signed releases for Linux and macOS (amd64 and arm64) and secure self-update
  with `istok update`.

[0.5.1]: https://github.com/vtimame/istok.sh/compare/v0.4.0...v0.5.1
[0.4.0]: https://github.com/vtimame/istok.sh/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/vtimame/istok.sh/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/vtimame/istok.sh/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/vtimame/istok.sh/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/vtimame/istok.sh/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/vtimame/istok.sh/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/vtimame/istok.sh/releases/tag/v0.1.0
