# Istok CLI

Istok is a local-first CLI and MCP server for project context, tasks, execution
runs, artifacts, and validation evidence. The core workflow is autonomous: it
uses an embedded SQLite database and does not require an account or cloud
connection.

The current release channel is a closed alpha. Linux and macOS are supported on
`amd64` and `arm64`. Windows binaries are not published yet.

## Install a release

Choose a stable version from [GitHub Releases][releases], then download the
archive for your platform. For example, on Linux `amd64`:

```sh
VERSION=v0.1.0
ASSET=istok_linux_amd64.tar.gz

curl -fLO "https://github.com/s26-dev/Istok-CLI/releases/download/${VERSION}/${ASSET}"
curl -fLO "https://github.com/s26-dev/Istok-CLI/releases/download/${VERSION}/SHA256SUMS"
grep "  ${ASSET}$" SHA256SUMS | sha256sum --check
tar -xzf "${ASSET}"
install -m 0755 istok "$HOME/.local/bin/istok"
istok version
```

Use `istok_darwin_arm64.tar.gz` on Apple silicon, `istok_darwin_amd64.tar.gz`
on Intel macOS, and `istok_linux_arm64.tar.gz` on Linux ARM64. On macOS, replace
the checksum command with:

```sh
grep "  ${ASSET}$" SHA256SUMS | shasum -a 256 --check
```

Make sure the installation directory is in `PATH`. Every release also contains
an ECDSA signature next to each archive; `istok update` verifies that signature
against the certificate embedded in the installed binary.

## Quick start

Initialize the current directory and add project knowledge and a task:

```sh
istok init . --name my-project
istok context add "Build policy" \
  --kind instruction \
  --body "Run unit tests before completing implementation tasks."
istok task create \
  --title "Implement the next change" \
  --acceptance-criteria "Implementation and validation are complete."
```

Inspect the current work and export all active project context as Markdown:

```sh
istok task list
istok task ready
istok context show
```

Repository indexing is automatic. Search and inspect the local code graph from
inside the project:

```sh
istok index status
istok search "query"
istok graph symbol SymbolName
```

Start the MCP server for an agent from inside the project:

```sh
istok mcp --profile worker
```

Use `istok <command> --help` for the complete command contract. Shell completion
is available through `istok completion bash|fish|zsh`.

## Updates and data

Stable builds can check and apply signed releases:

```sh
istok update --check
istok update --yes
```

The local database is stored in the platform user data directory by default.
Set `ISTOK_DATABASE` or pass `--database PATH` to use another database. Back up
that file before testing alpha releases with important project data.

Istok currently covers the local task/context/run workflow, automatic repository
indexing, lexical search, and a lightweight code graph. Cloud sync and a web
interface are not part of this release.

## Index acceptance

Contributors can run the indexing benchmark and process-level hardening suite
with Docker Compose v2:

```sh
make test-index-benchmark
make test-index-heavy
make test-index-release
```

The container has no network access and runs with bounded CPU, memory, and PID
resources. Reports are written to `tmp/index-acceptance`. The release target
requires a clean Git worktree and records the exact commit, container image,
corpus hash, quality metrics, timings, and sidecar size. These Linux container
tests complement, but do not replace, native release-binary smoke tests on Linux
and macOS. The release workflow builds each archive on a matching native runner,
extracts it, and verifies version reporting, indexing, lexical search, Go and
TypeScript graph lookup, and system-library dependencies before publication.

[releases]: https://github.com/s26-dev/Istok-CLI/releases
