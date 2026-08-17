#!/usr/bin/env bash

set -euo pipefail

mode="${1:-heavy}"

mkdir -p /reports /tmp/go-build /tmp/go-tmp

case "$mode" in
heavy)
    go test -tags=hardening -count=1 -json ./tests/hardening \
        | tee /reports/hardening-events.json

    go run ./tools/indexbench \
        --config testdata/indexing/benchmark.json \
        --output /reports/index-acceptance.json \
        --commit "${ISTOK_COMMIT:-unknown}" \
        --dirty="${ISTOK_DIRTY:-true}" \
        --hardening-events /reports/hardening-events.json
    ;;
benchmark)
    go run ./tools/indexbench \
        --config testdata/indexing/benchmark.json \
        --output /reports/index-benchmark.json \
        --commit "${ISTOK_COMMIT:-unknown}" \
        --dirty="${ISTOK_DIRTY:-true}"
    ;;
*)
    printf 'unknown hardening mode: %s\n' "$mode" >&2
    exit 2
    ;;
esac
