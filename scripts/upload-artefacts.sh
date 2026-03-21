#!/usr/bin/env bash
#
# Upload cattery model/voice/ORT artefacts to jikkuatwork/cattery-artefacts
#
# Prerequisites:
#   - gh auth login
#   - models-data/ populated (run the spike first)
#
# Usage: ./scripts/upload-artefacts.sh
set -euo pipefail

REPO="jikkuatwork/cattery-artefacts"
TAG="v0.1.0"
DATA_DIR="models-data"

echo "=== Preparing release $TAG for $REPO ==="

# Collect assets
ASSETS=(
  "$DATA_DIR/onnx/model_quantized.onnx"
  "$DATA_DIR/voices/af_heart.bin"
)

# Add all voices if present
for f in "$DATA_DIR"/voices/*.bin; do
  [[ -f "$f" ]] && ASSETS+=("$f")
done

# Deduplicate
ASSETS=($(printf '%s\n' "${ASSETS[@]}" | sort -u))

# Verify files exist
for f in "${ASSETS[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "ERROR: Missing $f"
    exit 1
  fi
done

# Generate checksums
echo "=== SHA256 checksums ==="
sha256sum "${ASSETS[@]}" | tee /tmp/cattery-sha256sums.txt
ASSETS+=("/tmp/cattery-sha256sums.txt")

# Create release and upload
echo ""
echo "=== Creating release $TAG ==="
gh release create "$TAG" \
  --repo "$REPO" \
  --title "Cattery Artefacts $TAG" \
  --notes "$(cat <<'EOF'
Model and voice files for [cattery](https://github.com/jikkuatwork/cattery).

## Contents
- `model_quantized.onnx` — Kokoro-82M int8 quantized ONNX model (92MB)
- `*.bin` — Voice style vectors (510KB each)
- `SHA256SUMS` — Checksums for all files

## Source
- Model: [onnx-community/Kokoro-82M-v1.0-ONNX](https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX)
- License: Apache-2.0

## ORT Runtime
ONNX Runtime is downloaded directly from Microsoft:
https://github.com/microsoft/onnxruntime/releases
EOF
)" \
  "${ASSETS[@]}"

echo ""
echo "=== Done! Release at: https://github.com/$REPO/releases/tag/$TAG ==="
