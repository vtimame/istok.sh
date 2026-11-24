# Prepares the scene for proof.tape: a throwaway copy of the splitbill demo
# project in its own Istok database, with a task whose `go test` already
# passed through Istok. Sourced by the tape, so it exports what the visible
# commands use: RUN, CHECK and REV.
#
# ISTOK_DEMO_SOURCE is a directory with the splitbill go.mod, split.go and
# split_test.go (default: ../istok-demo next to this repository).

set -e

source_dir="${ISTOK_DEMO_SOURCE:-$PWD/../istok-demo}"
scene="$(mktemp -d)"

export PATH="$PWD/bin:$PATH"
export ISTOK_DATABASE="$scene/istok.db"

mkdir "$scene/splitbill"
cp "$source_dir/go.mod" "$source_dir/split.go" "$source_dir/split_test.go" "$scene/splitbill/"
cd "$scene/splitbill"

git init -q
git add -A
git -c user.email=demo@istok.sh -c user.name=demo commit -qm "Split a bill between people"

istok init >/dev/null
istok task create --title "Round leftover cents fairly" >/dev/null

claim="$(istok task claim 1 --json)"
RUN="$(printf '%s' "$claim" | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])')"
LEASE="$(printf '%s' "$claim" | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; l=r.get("lease"); print(l["id"] if isinstance(l, dict) else r["lease_id"])')"

CHECK="$(istok run validate --json --lease="$LEASE" "$RUN" -- go test ./... |
	python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(r.get("validation", r)["id"])')"
REV="$(istok task show 1 --json |
	python3 -c 'import json,sys; t=json.load(sys.stdin)["result"]["task"]; print(t.get("task", t)["revision"])')"

export RUN LEASE CHECK REV

set +e
