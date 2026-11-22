#!/bin/sh
# Renders the npm package "istok-cli" for a release into a directory.
#
#   scripts/npm-package.sh v0.5.5 dist/npm
#
# The package holds no binary: its "istok" command is the plugin launcher,
# pinned to this release. On first run it downloads the release archive for
# the platform, checks its SHA-256 and its signature, and runs it. That lets
# MCP clients start Istok with "npx istok-cli@0.5.5 mcp".

set -eu

tag="${1:?usage: npm-package.sh vX.Y.Z output-dir}"
out="${2:?usage: npm-package.sh vX.Y.Z output-dir}"

printf '%s\n' "$tag" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+' || {
  echo "npm-package.sh: $tag is not a release tag like v0.5.5" >&2
  exit 1
}
version="${tag#v}"

root="$(cd "$(dirname "$0")/.." && pwd)"

rm -rf "$out"
mkdir -p "$out/bin"

# The launcher, pinned to this release. Both lines must exist exactly once,
# or the package would quietly download a different release.
launcher="$root/plugins/istok/bin/istok"
[ "$(grep -c '^MIN_VERSION="' "$launcher")" -eq 1 ] || { echo "npm-package.sh: MIN_VERSION not found once in the launcher" >&2; exit 1; }
[ "$(grep -c '^RELEASE_TAG="' "$launcher")" -eq 1 ] || { echo "npm-package.sh: RELEASE_TAG not found once in the launcher" >&2; exit 1; }

sed -e "s/^MIN_VERSION=\".*\"$/MIN_VERSION=\"$version\"/" \
    -e "s/^RELEASE_TAG=\".*\"$/RELEASE_TAG=\"$tag\"/" \
    -e 's#^DATA_DIR=.*#DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/istok/npm"#' \
    "$launcher" > "$out/bin/istok"
chmod 0755 "$out/bin/istok"

cp "$root/LICENSE" "$out/LICENSE"

jq -n --arg version "$version" '{
  name: "istok-cli",
  version: $version,
  description: "Local task tracker and MCP server for coding agents: Istok runs the agent'"'"'s checks itself and keeps the proof behind \"done\".",
  mcpName: "io.github.vtimame/istok",
  bin: { istok: "bin/istok", "istok-cli": "bin/istok" },
  files: ["bin/istok", "LICENSE", "README.md"],
  os: ["darwin", "linux"],
  cpu: ["x64", "arm64"],
  keywords: ["mcp", "mcp-server", "coding-agent", "tasks", "claude-code", "codex", "cursor", "local-first"],
  homepage: "https://istok.sh",
  repository: { type: "git", url: "git+https://github.com/vtimame/istok.sh.git" },
  bugs: { url: "https://github.com/vtimame/istok.sh/issues" },
  license: "Apache-2.0",
  author: { name: "Timur Valitov", url: "https://github.com/vtimame" }
}' > "$out/package.json"

cat > "$out/README.md" <<EOF
# istok-cli

Istok is a local task tracker and MCP server for coding agents. The agent
records tasks and runs through MCP and runs its checks through Istok, which
executes the command itself and keeps the exit code and output as evidence.
Everything stays in one SQLite file on your machine.

This package starts Istok $version for MCP clients:

\`\`\`json
{
  "mcpServers": {
    "istok": { "command": "npx", "args": ["-y", "istok-cli@$version", "mcp"] }
  }
}
\`\`\`

Start the client in your project directory: Istok binds the MCP session to it.

The package holds no binary. On first run it downloads the Istok $version
release for your platform from GitHub, checks its SHA-256 checksum and its
release signature, and keeps it in \`~/.local/share/istok/npm\`. An \`istok\`
on PATH of version $version or newer is used instead.

Linux and macOS, amd64 and arm64. It needs curl, tar and openssl.

Other ways to install, such as Homebrew and the Claude Code plugin, and the
documentation: https://istok.sh
EOF
