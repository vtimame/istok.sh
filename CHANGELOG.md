# Changelog

All notable changes to Istok are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Istok uses
[Semantic Versioning](https://semver.org/).

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

[0.3.0]: https://github.com/vtimame/istok.sh/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/vtimame/istok.sh/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/vtimame/istok.sh/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/vtimame/istok.sh/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/vtimame/istok.sh/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/vtimame/istok.sh/releases/tag/v0.1.0
