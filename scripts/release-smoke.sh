#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 5 ]; then
  printf 'usage: %s ARCHIVE VERSION COMMIT GOOS GOARCH\n' "$0" >&2
  exit 2
fi

archive=$1
version=$2
commit=$3
goos=$4
goarch=$5

fail() {
  printf 'release smoke: %s\n' "$*" >&2
  exit 1
}

require_match() {
  value=$1
  pattern=$2
  description=$3

  printf '%s\n' "$value" | grep -F -- "$pattern" >/dev/null || fail "missing $description"
}

host_os=$(uname -s)
host_arch=$(uname -m)

case "$host_os" in
  Linux) native_os=linux ;;
  Darwin) native_os=darwin ;;
  *) fail "unsupported host OS $host_os" ;;
esac

case "$host_arch" in
  x86_64) native_arch=amd64 ;;
  arm64|aarch64) native_arch=arm64 ;;
  *) fail "unsupported host architecture $host_arch" ;;
esac

[ "$goos" = "$native_os" ] || fail "archive target OS $goos does not match host $native_os"
[ "$goarch" = "$native_arch" ] || fail "archive target architecture $goarch does not match host $native_arch"
[ -f "$archive" ] || fail "archive not found: $archive"
[ -n "$commit" ] || fail 'commit must not be empty'

workdir=$(mktemp -d "${TMPDIR:-/tmp}/istok-release-smoke.XXXXXX")
ui_pid=
cleanup() {
  [ -z "$ui_pid" ] || kill "$ui_pid" 2>/dev/null || true
  rm -rf "$workdir"
}
trap cleanup EXIT HUP INT TERM

archive_entries=$(tar -tzf "$archive")
[ "$archive_entries" = istok ] || fail 'archive must contain exactly one istok executable'

tar -xzf "$archive" -C "$workdir"
binary="$workdir/istok"
[ -f "$binary" ] && [ -x "$binary" ] || fail 'archive entry istok is not executable'

printf 'archive: %s\n' "$archive"
printf 'target: %s/%s\n' "$goos" "$goarch"
printf 'commit: %s\n' "$commit"
printf 'version: %s\n' "$version"

actual_version=$("$binary" version)
[ "$actual_version" = "$version" ] || fail "istok version is $actual_version, expected $version"

executable=$(file "$binary")
printf 'executable: %s\n' "$executable"

case "$goos/$goarch" in
  linux/amd64)
    require_match "$executable" 'ELF 64-bit' 'Linux executable format'
    printf '%s\n' "$executable" | grep -E 'x86[-_]64' >/dev/null || fail 'Linux executable is not amd64'
    ;;
  linux/arm64)
    require_match "$executable" 'ELF 64-bit' 'Linux executable format'
    printf '%s\n' "$executable" | grep -Ei 'aarch64|arm64' >/dev/null || fail 'Linux executable is not arm64'
    ;;
  darwin/amd64)
    require_match "$executable" 'Mach-O 64-bit executable x86_64' 'Darwin amd64 executable format'
    ;;
  darwin/arm64)
    require_match "$executable" 'Mach-O 64-bit executable arm64' 'Darwin arm64 executable format'
    ;;
  *)
    fail "unsupported release target $goos/$goarch"
    ;;
esac

build_metadata=$(go version -m "$binary")
printf 'build metadata:\n%s\n' "$build_metadata"
require_match "$build_metadata" "CGO_ENABLED=1" 'CGO build metadata'
require_match "$build_metadata" "GOOS=$goos" 'GOOS build metadata'
require_match "$build_metadata" "GOARCH=$goarch" 'GOARCH build metadata'
require_match "$build_metadata" "vcs.revision=$commit" 'VCS revision build metadata'
require_match "$build_metadata" 'vcs.modified=false' 'clean VCS build metadata'

check_linux_dependencies() {
  dependencies=$(ldd "$binary")
  printf 'dependencies:\n%s\n' "$dependencies"

  printf '%s\n' "$dependencies" | grep -F 'not found' >/dev/null && fail 'dynamic dependency is missing'

  while IFS= read -r dependency; do
    [ -n "$dependency" ] || continue
    dependency=$(printf '%s\n' "$dependency" | sed 's/^[[:space:]]*//')

    case "$dependency" in
      linux-vdso.so.*|/lib*/ld-linux*.so.*|/usr/lib*/ld-linux*.so.*)
        continue
        ;;
    esac

    library=$(printf '%s\n' "$dependency" | sed 's/^[[:space:]]*//' | sed 's/[[:space:]].*$//')
    resolved=$(printf '%s\n' "$dependency" | sed -n 's/.*=>[[:space:]]*\([^[:space:]]*\).*/\1/p')

    case "$library" in
      libc.so.*|libresolv.so.*|libpthread.so.*|libdl.so.*|libm.so.*|librt.so.*|libgcc_s.so.*)
        ;;
      *)
        fail "disallowed Linux dependency: $dependency"
        ;;
    esac

    case "$resolved" in
      /lib/*|/lib64/*|/usr/lib/*|/usr/lib64/*)
        ;;
      *)
        fail "non-system Linux dependency: $dependency"
        ;;
    esac
  done <<EOF
$dependencies
EOF
}

check_macos_dependencies() {
  dependencies=$(otool -L "$binary")
  printf 'dependencies:\n%s\n' "$dependencies"

  printf '%s\n' "$dependencies" | sed '1d' | while IFS= read -r dependency; do
    library=$(printf '%s\n' "$dependency" | sed 's/^[[:space:]]*//' | sed 's/[[:space:]].*$//')

    case "$library" in
      /usr/lib/*|/System/Library/*)
        ;;
      @rpath/*|/opt/*|/usr/local/*)
        fail "disallowed macOS dependency: $library"
        ;;
      *)
        fail "non-system macOS dependency: $library"
        ;;
    esac
  done
}

case "$goos" in
  linux) check_linux_dependencies ;;
  darwin) check_macos_dependencies ;;
esac

export HOME="$workdir/home"
export TMPDIR="$workdir/tmp"
export XDG_CACHE_HOME="$workdir/cache"
export XDG_DATA_HOME="$workdir/data-home"
export ISTOK_DATABASE="$workdir/data/istok.db"
export ISTOK_INDEX_ROOT="$workdir/indexes"
mkdir -p "$HOME" "$TMPDIR" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$workdir/data" "$ISTOK_INDEX_ROOT" "$workdir/project"

cat > "$workdir/project/smoke.go" <<'EOF'
package smoke

func ReleaseGoSymbol() string {
	return "native-search-token"
}
EOF

cat > "$workdir/project/smoke.ts" <<'EOF'
export function releaseTypeScriptSymbol(): string {
  return "native-ts-token";
}
EOF

cd "$workdir/project"

init_json=$("$binary" init --json)
status_json=$("$binary" index status --json)
search_json=$("$binary" search native-search-token --json)
go_graph_json=$("$binary" graph symbol ReleaseGoSymbol --json)
ts_graph_json=$("$binary" graph symbol releaseTypeScriptSymbol --json)

require_match "$init_json" '"schema_version":"1"' 'init schema version'
require_match "$status_json" '"schema_version":"1"' 'index status schema version'
require_match "$status_json" '"state":"ready"' 'ready index state'
require_match "$search_json" '"schema_version":"1"' 'search schema version'
require_match "$search_json" '"contract_version":"istok.retrieval.v1"' 'retrieval contract version'
require_match "$search_json" '"path":"smoke.go"' 'search result path'
require_match "$search_json" '"symbol":"smoke.ReleaseGoSymbol"' 'search result symbol'
require_match "$go_graph_json" '"schema_version":"1"' 'Go graph schema version'
require_match "$go_graph_json" '"contract_version":"istok.graph.v1"' 'graph contract version'
require_match "$go_graph_json" '"path":"smoke.go"' 'Go graph path'
require_match "$go_graph_json" '"name":"ReleaseGoSymbol"' 'Go graph symbol'
require_match "$ts_graph_json" '"schema_version":"1"' 'TypeScript graph schema version'
require_match "$ts_graph_json" '"contract_version":"istok.graph.v1"' 'TypeScript graph contract version'
require_match "$ts_graph_json" '"path":"smoke.ts"' 'TypeScript graph path'
require_match "$ts_graph_json" '"name":"releaseTypeScriptSymbol"' 'TypeScript graph symbol'

# The release must embed the built web UI, not the placeholder that a plain
# go build without the frontend produces.
ui_log="$workdir/ui.log"
"$binary" ui --port 0 --no-open >"$ui_log" 2>&1 &
ui_pid=$!

ui_url=
for _ in $(seq 1 100); do
  ui_url=$(sed -n 's/^Istok UI: //p' "$ui_log")
  [ -n "$ui_url" ] && break
  sleep 0.1
done
[ -n "$ui_url" ] || fail "istok ui did not start: $(cat "$ui_log")"

ui_index=$(curl -fsS "$ui_url")
ui_health=$(curl -fsS "${ui_url}api/v1/health")
kill "$ui_pid" 2>/dev/null || true
wait "$ui_pid" 2>/dev/null || true
ui_pid=

printf 'web UI: %s\n' "$ui_url"
require_match "$ui_index" '<div id="root">' 'embedded web UI'
printf '%s\n' "$ui_index" | grep -F 'web UI is not built' >/dev/null && fail 'release embeds the web UI placeholder'
require_match "$ui_health" "\"version\":\"$version\"" 'web UI health version'

printf 'release smoke passed\n'
