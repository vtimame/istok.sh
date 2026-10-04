<p align="center">
  <img src=".github/assets/istok.svg" width="88" alt="Istok">
</p>

<h1 align="center">Istok</h1>

<p align="center">
  Durable project state for coding agents.
</p>

<p align="center">
  <a href="https://istok.sh">Documentation</a>
  ·
  <a href="https://github.com/vtimame/istok.sh/releases">Releases</a>
</p>

<p align="center">
  <a href="https://github.com/vtimame/istok.sh/releases">
    <img src="https://img.shields.io/github/v/release/vtimame/istok.sh?style=flat-square" alt="Release">
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="Apache 2.0">
  </a>
  <a href="https://istok.sh">
    <img src="https://img.shields.io/badge/docs-istok.sh-green?style=flat-square" alt="Documentation">
  </a>
</p>

Istok is a local CLI and MCP server that keeps coding-agent work with your project instead of inside a conversation.

Tasks, context, progress, runs, validation, and repository knowledge remain available when a session ends, when you start a new conversation, or when another coding agent takes over.

```text
Coding agent
     │
     │ MCP
     ▼
   Istok
     │
     ├── tasks
     ├── context
     ├── runs
     ├── validation
     └── repository retrieval
     │
     ▼
  your project
```

Istok is not another backlog or issue tracker. It is a local execution layer for work that has already reached your coding agent.

## Install

```sh
curl -fsSL https://get.istok.sh | sh
```

The installer detects your operating system and architecture, verifies the downloaded release, and installs `istok` into:

```text
$HOME/.local/bin
```

No elevated privileges are required for the default installation.

Verify the installation:

```sh
istok version
```

You can inspect the installation script before running it:

```sh
curl -fsSL https://get.istok.sh -o install-istok.sh
less install-istok.sh
sh install-istok.sh
```

See the [installation guide](https://istok.sh/docs/en/getting-started/installation) for manual installation and custom install directories.

## Quick start

Initialize Istok in a repository:

```sh
cd my-project
istok init
```

You only need to initialize a repository once.

Istok exposes its tools to coding agents through MCP:

```sh
istok mcp
```

Give each coding agent a stable identity:

```sh
istok mcp \
  --actor-id codex \
  --actor-name "Codex"
```

For example, with Codex:

```sh
codex mcp add istok -- \
  istok mcp \
  --actor-id codex \
  --actor-name "Codex"
```

Istok can also be used with Claude Code, Cursor, Antigravity CLI, and other MCP clients.

See the [quick start](https://istok.sh/docs/en/getting-started/quick-start) for agent-specific setup and recommended instructions.

Once configured, continue working with your coding agent normally.

You generally do **not** need to manually create tasks, claim runs, prepare context, or record validation. An agent configured to use Istok can do that through MCP as part of its workflow.

## Why Istok

Coding-agent conversations are temporary. The work behind them often is not.

A task may:

* span several sessions;
* require multiple attempts;
* become blocked by another change;
* move between different coding agents;
* need validation before it can be considered complete.

Without durable state, useful information tends to remain scattered across conversation history.

Istok keeps that state with the project.

```text
Session A
   │
   ├── task
   ├── progress
   ├── context
   ├── run
   └── validation
         │
         ▼
       Istok
         │
         ▼
Session B
```

A new session can inspect what happened before without reconstructing the previous conversation.

The same applies when switching agents:

```text
Codex
   │
   ▼
Istok project state
   │
   ▼
Claude Code
```

## Core concepts

### Projects

A project associates Istok state with a repository.

```sh
istok init
```

Project state stays local and can be inspected independently of any coding-agent session.

Projects may be nested. This is useful in monorepositories where one agent coordinates
the whole repository while other agents work within individual packages:

```text
my-monorepo/             # monorepo project
└── packages/
    ├── api/             # independent API project
    └── web/             # independent web project
```

Run `istok init` at each exact root that needs independent tasks, context, runs, and
validation. Commands select the project with the deepest root containing the current
working directory. The monorepo index covers the complete repository, while a package
index is limited to that package.

### Tasks

Tasks are durable units of non-trivial work.

They can preserve:

* goals and acceptance criteria;
* progress;
* dependencies and blockers;
* decisions and comments;
* previous attempts;
* validation associated with the work.

Small questions and lightweight consultations do not need tasks.

### Context

Istok keeps durable project knowledge outside the conversation.

This includes project notes, decisions, constraints, reusable instructions, and repository context relevant to the work.

Task runs assemble context through three explicit delivery modes:

* `always` for small project-wide instructions and constraints;
* `ranked` for records selected by task relevance;
* `manual` for records included only through an explicit context ID.

The default run snapshot starts with a 12-record durable item budget and is bounded to
16 KiB of durable context, 32 KiB of repository retrieval, and a 48 KiB combined
ceiling. When more than 12 active `always` records are required, Istok automatically
expands only the item budget up to the 64-record absolute limit; byte and per-record
limits remain enforced. An explicit `--context-limit` is never expanded and fails with
a suggested minimum when it cannot contain all required `always` records. Selection
metadata in snapshot schema v5 records the effective item limit and its reason together
with lanes, scores, matched terms, candidate hashes, byte budgets, usage, warnings, and
a separately bounded knowledge briefing. Existing snapshot schemas v1-v4 remain readable.

Context records can also carry `review_after`, `expires_at`, and `superseded_by`
lifecycle metadata. Expired and superseded records are excluded from automatic
assembly; records due for review remain usable but produce warnings.

Preview and diagnose the result before claiming a task:

```sh
istok context preview 42
istok context doctor
```

Use repeated `--context-id` flags for required manual context. The legacy all-context
path is intentionally explicit and audited:

```sh
istok task claim 42 \
  --all-context \
  --context-override-reason "one-time migration audit"
```

### Pull-first knowledge

Knowledge is durable project documentation managed by Istok, but it is not pushed into
every agent run. Catalog and search operations return bounded metadata and short snippets;
the full body is loaded only through `show` or `read`:

```sh
istok knowledge add "Authentication" \
  --kind architecture \
  --summary "Current authentication contract" \
  --body-file ./auth-notes.md

istok knowledge list --status current
istok knowledge search "JWKS cache"
istok knowledge read <knowledge-id>
```

New and distilled items start as drafts. Review is explicit, as are promotion and
supersession:

```sh
istok knowledge distill "Authentication v2" \
  --summary "Replacement contract" \
  --source run:<run-id>
istok knowledge review <knowledge-id> --expected-revision 1 --note "Verified against code"
istok knowledge promote <knowledge-id> --expected-revision 2
istok knowledge supersede <old-id> <replacement-id> --expected-revision 3
```

Editing a reviewed draft clears its review evidence; it must be reviewed again before
promotion. A draft replacement must also be reviewed before it can supersede current
knowledge. Current and superseded items are immutable; changes go through a new draft so
agents never observe an unreviewed edit as current truth.

Istok-managed bodies live in the same XDG-backed SQLite storage as other local Istok
state, never in the project root. Repository documents remain developer-owned and may be
referenced through provenance without being modified. Markdown is an explicit export
format, not the canonical store:

```sh
istok knowledge export <knowledge-id> --output /explicit/path/authentication.md
```

Exports require a caller-supplied destination, are published atomically, and refuse to
overwrite an existing file. Run snapshots contain at most 20 current knowledge summaries
within a separate 4 KiB budget; they never contain knowledge bodies or provenance.

### Runs

A task describes **what needs to be done**.

A run describes **one attempt to do it**.

Keeping them separate means a task can survive interrupted or failed attempts without losing their history.

### Validation

Istok records how work was checked instead of relying only on an agent saying that it works.

```text
Task
  ↓
Run
  ↓
Implementation
  ↓
Validation
  ↓
Evidence
```

Failed validation also remains part of the work history and can inform the next attempt.

### Repository retrieval

Istok maintains a local repository index that coding agents can use while working.

The CLI exposes the same capabilities directly:

```sh
istok search "authentication timeout"
istok graph symbol AuthService
istok graph neighbors AuthService
```

Index freshness is handled automatically during normal workflows.

## CLI

The CLI gives you direct access to the same local project state exposed to coding agents through MCP.

| Command            | Purpose                                             |
| ------------------ | --------------------------------------------------- |
| `istok init`       | Initialize a repository as an Istok project         |
| `istok mcp`        | Start the MCP server used by coding agents          |
| `istok project`    | Inspect and manage projects                         |
| `istok task`       | Inspect and manage tasks                            |
| `istok run`        | Inspect runs, executions, artifacts, and validation |
| `istok context`    | Inspect and manage durable project context          |
| `istok knowledge`  | Pull, review, and export durable project knowledge  |
| `istok index`      | Inspect or rebuild the repository index             |
| `istok search`     | Search indexed repository content                   |
| `istok graph`      | Explore symbols and code relationships              |
| `istok update`     | Check for and install updates                       |
| `istok completion` | Configure shell completion                          |
| `istok version`    | Print version information                           |

Start with:

```sh
istok --help
```

and inspect individual commands with:

```sh
istok task --help
istok task show --help
```

Human-readable output is used by default. Supported commands can also return versioned JSON for scripting and integrations:

```sh
istok task list --json
```

## Local first

Istok is designed to work locally.

The current project state is stored in SQLite, while repository indexes and managed artifacts are kept in separate local storage.

By default, the database is stored at
`${XDG_DATA_HOME:-$HOME/.local/share}/istok/istok.db` on Linux and
`$HOME/Library/Application Support/istok/istok.db` on macOS. Istok keeps its
default data directory at POSIX mode `0700` and creates or repairs the database
as a regular file at mode `0600`. A custom `--database` path is supported, but
Istok does not change the permissions of an already existing parent directory,
rejects one writable by group or others, and refuses a final path that is a
symbolic link. Existing platform-specific extended ACL entries are not rewritten.

Normal use does not require:

* a cloud account;
* an external database;
* a background server;
* network access after installation.

The local project remains the source of durable execution state used by both the CLI and MCP.

## Multiple coding agents

Istok state is not tied to a particular agent.

Stable actor IDs let recorded work remain attributable across MCP restarts and between different agents:

```text
Task
 ├── Run by codex
 │    └── validation
 │
 └── Run by claude
      └── validation
```

This makes it possible for one agent to start work and another to inspect or continue it later.

## Supported platforms

Current releases target:

* Linux x86-64
* Linux ARM64
* macOS Intel
* macOS Apple Silicon

Windows is not part of the current supported release matrix.

## Update

Istok can update itself to a newer stable release:

```sh
istok update
```

Check for an update without installing it:

```sh
istok update --check
```

Release archives are signed and verified before an update is applied.

## Build from source

Istok is written in Go and uses CGO for its embedded SQLite integration.

Build the binary:

```sh
make build
```

The result is written to:

```text
bin/istok
```

Run the test suite:

```sh
go test ./...
```

## Documentation

Full documentation is available at:

**https://istok.sh**

Useful starting points:

* [Introduction](https://istok.sh/docs/en/getting-started/introduction)
* [Installation](https://istok.sh/docs/en/getting-started/installation)
* [Quick start](https://istok.sh/docs/en/getting-started/quick-start)
* [How Istok works](https://istok.sh/docs/en/how-istok-works/projects)
* [CLI reference](https://istok.sh/docs/en/reference/cli-overview)
* [MCP reference](https://istok.sh/docs/en/reference/mcp)
* [Troubleshooting](https://istok.sh/docs/en/help/troubleshooting)

Releases are published on [GitHub Releases](https://github.com/vtimame/istok.sh/releases).
