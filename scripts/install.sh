#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

echo "Building cattery..."
cd "$REPO_ROOT"
go build -o "$INSTALL_DIR/cattery" ./cmd/cattery

echo "Installed: $INSTALL_DIR/cattery"
"$INSTALL_DIR/cattery" --version 2>/dev/null || true
