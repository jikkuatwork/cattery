#!/usr/bin/env bash
# bench.sh — profile cattery server at different worker counts.
# Captures RSS, heap, latency, and throughput.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT=7199
BINARY=./cattery

echo "Building cattery..."
go build -o "$BINARY" ./cmd/cattery

for WORKERS in 1 2 3; do
    echo ""
    echo "========================================="
    echo " Workers: $WORKERS"
    echo "========================================="

    # Start server in background
    "$BINARY" serve --port "$PORT" -w "$WORKERS" &
    SERVER_PID=$!

    # Wait for server to be ready
    for i in $(seq 1 30); do
        if curl -s "http://localhost:$PORT/v1/status" > /dev/null 2>&1; then
            break
        fi
        sleep 0.5
    done

    # Capture RSS before
    RSS_BEFORE=$(ps -o rss= -p "$SERVER_PID" 2>/dev/null | tr -d ' ')
    echo "RSS before: $((RSS_BEFORE / 1024)) MB"

    # Run load test
    BENCH_PORT="$PORT" go run scripts/bench.go

    # Capture RSS after
    sleep 1
    RSS_AFTER=$(ps -o rss= -p "$SERVER_PID" 2>/dev/null | tr -d ' ')
    echo "RSS after:  $((RSS_AFTER / 1024)) MB"

    # Capture goroutine count
    GOROUTINES=$(curl -s "http://localhost:$PORT/debug/pprof/goroutine?debug=1" | head -1)
    echo "Goroutines: $GOROUTINES"

    # Kill server
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    sleep 1
done

echo ""
echo "Done."
