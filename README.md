<p align="center">
  <img src=".github/assets/istok.svg" width="88" alt="Istok">
</p>

<h1 align="center">Istok</h1>

<p align="center">
  One workspace for every coding agent.
</p>

<p align="center">
  <a href="https://istok.sh">Website</a>
  ·
  <a href="https://istok.sh/docs/en/getting-started/quick-start">Quick start</a>
  ·
  <a href="https://istok.sh/docs/ru/getting-started/quick-start">Документация на русском</a>
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

Your coding agent says "done". Istok keeps the proof: the task, every attempt, and the checks behind the result, with Istok running the commands itself. It all stays with your project instead of inside one conversation, so a new session — or a different agent, or another of your machines — picks up where the last one stopped.

<p align="center">
  <img src=".github/assets/demo.gif" width="760" alt="istok task show: a task Claude Code finished, with go build and go test run by Istok">
</p>

<p align="center"><sub>A bug fix Claude Code made in a sample project. It could not run the tests, said so and left the task open; then Istok ran <code>go build</code> and <code>go test</code> itself, and the task closed.</sub></p>

## Why

If you work with Claude Code, Codex or Cursor, three things keep getting in the way:

- **Sessions end.** Close the chat or hit the context limit, and the plan, the decisions and the half-finished work go with it.
- **Work is invisible.** With several agents in several repositories, it is hard to tell who did what and what quietly stalled.
- **"Done" is a claim.** The agent says the tests pass; nothing shows that it ran them.

Istok is a local CLI and MCP server. Your agent records its work through MCP while it goes; you keep talking to it as before. Everything stays on your machine in one SQLite file: no account, no cloud, and nothing written into your repository. To work on several machines, run your own sync hub.

## Quick start

**1. Install** (Linux and macOS, no root needed):

```sh
curl -fsSL https://get.istok.sh | sh
```

**2. Connect your agent** once, for your user. For Claude Code:

```sh
claude mcp add --transport stdio --scope user istok -- \
  istok mcp --actor-id claude --actor-name "Claude Code"
```

For Codex:

```sh
codex mcp add istok -- \
  istok mcp --actor-id codex --actor-name "Codex"
```

To keep work across restarts and compactions, add the hooks once:

```sh
istok hooks install claude   # or: istok hooks install codex
```

The server lists the core workflow tools; start it with `istok mcp --tools full` for every tool.

Cursor and other MCP clients are covered in the [quick start](https://istok.sh/docs/en/getting-started/quick-start), together with a short block of instructions that tells the agent when to use Istok.

**3. Ask the agent to register your project.** Start it in the repository and say:

> Register this project in Istok.

Then give it real work as usual. It creates a task, claims it, runs the checks through Istok and completes the task with that evidence.

**4. See what happened:**

```sh
istok task list
istok task show 1
```

Each task shows its history, with every run and the linked commits.

## Learn more

- [What is Istok](https://istok.sh/docs/en/getting-started/introduction) — the problem and what you get.
- [Working with your agent](https://istok.sh/docs/en/guides/working-with-agents) — what to say to continue work, remember a rule or hand over to another agent.
- [Sync between devices](https://istok.sh/docs/en/guides/another-device) — run a hub and continue the same project on another machine.
- [How Istok works](https://istok.sh/docs/en/how-istok-works/overview) — projects, tasks and runs, validation, context and knowledge.
- [CLI](https://istok.sh/docs/en/reference/cli) and [MCP tools](https://istok.sh/docs/en/reference/mcp) reference.
- [FAQ and troubleshooting](https://istok.sh/docs/en/help/faq).

The documentation is also available [in Russian](https://istok.sh/docs/ru/getting-started/introduction), and as [`llms.txt`](https://istok.sh/llms.txt) for agents.

## Sync between your devices

To continue the same project on another machine, run a sync hub on any server your devices reach — a VPS or a home server, no domain or certificate needed. The hub stores only rows encrypted on your devices. With Docker, take [`docker/hub/compose.yml`](./docker/hub/compose.yml):

```sh
ISTOK_HUB_ADVERTISE=203.0.113.10:7443 docker compose up -d
docker compose exec hub istok hub device add laptop   # prints a one-time invitation
```

To reach the hub from several networks, list its addresses separated by commas, for example a LAN and a Tailscale address; devices use whichever answers.

On the device, `istok sync join CODE` registers it and `istok sync enable` in a project folder starts syncing; the second device joins with `--key` from `istok sync key`, and `istok sync enable` in its clone of the repository fetches the same project. The image `ghcr.io/vtimame/istok-hub` is published with every release for x86-64 and ARM64.

## Platforms

Releases are built for Linux (x86-64, ARM64) and macOS (Intel, Apple Silicon). Windows is not supported yet.

Releases are signed. `istok update` checks for a newer stable release, verifies it, backs up the database and migrates it; `istok update --check` only reports.

## Build from source

Istok is written in Go. Building needs Go with cgo (for SQLite):

```sh
make build      # writes bin/istok
make install    # installs into ~/.local/bin
go test ./...
```

A source build cannot update itself; install an official release for `istok update`.

## License

[Apache 2.0](./LICENSE). Changes are listed in the [changelog](./CHANGELOG.md).
