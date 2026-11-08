#!/usr/bin/env bash
# Fetches the pinned llama.cpp (native/llama-cpp.env) into .cache/llama.cpp-<commit>/, checks
# the commit, and prints the directory. Used by build.sh and scripts/ios-build.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../native/llama-cpp.env
source "$ROOT/native/llama-cpp.env"
dir="$ROOT/.cache/llama.cpp-$LLAMA_COMMIT"
if [[ "$(git -C "$dir" rev-parse HEAD 2>/dev/null || true)" != "$LLAMA_COMMIT" ]]; then
  rm -rf "$dir" && mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" fetch -q --depth 1 https://github.com/ggml-org/llama.cpp "$LLAMA_COMMIT"
  git -C "$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD
  [[ "$(git -C "$dir" rev-parse HEAD)" == "$LLAMA_COMMIT" ]] || { echo "llama.cpp $LLAMA_TAG: wrong commit" >&2; exit 1; }
fi
echo "$dir"
