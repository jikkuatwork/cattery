#!/usr/bin/env bash
set -euo pipefail

MEM=${1:-4G}

mkdir -p tmp

echo "=== memtest under ${MEM} memory limit ==="
systemd-run --scope -p MemoryMax="$MEM" \
    go test -tags memtest ./memtest/ -v -timeout 600s 2>&1 | tee "tmp/memtest-${MEM}.log"
