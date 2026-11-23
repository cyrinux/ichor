#!/usr/bin/env bash
# Fetches the pinned llama.cpp (native/llama-cpp.env) into .cache/llama.cpp-<commit>/, checks
# the commit, and prints the directory (everything else goes to stderr). The fetch is retried
# on a network failure. Used by build.sh and scripts/ios-build.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../native/llama-cpp.env
source "$ROOT/native/llama-cpp.env"
dir="$ROOT/.cache/llama.cpp-$LLAMA_COMMIT"
if [[ "$(git -C "$dir" rev-parse HEAD 2>/dev/null || true)" != "$LLAMA_COMMIT" ]]; then
  rm -rf "$dir" && mkdir -p "$dir"
  git -C "$dir" init -q
  # A DNS or network blip on the runner must not fail the build: four attempts, 5, 15, 45 s apart.
  for pause in 5 15 45 0; do
    git -C "$dir" fetch -q --depth 1 https://github.com/ggml-org/llama.cpp "$LLAMA_COMMIT" && break
    [[ "$pause" -gt 0 ]] || { echo "llama.cpp $LLAMA_TAG: fetch failed 4 times" >&2; exit 1; }
    echo "llama.cpp $LLAMA_TAG: fetch failed, retrying in $pause s" >&2
    sleep "$pause"
  done
  git -C "$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD
  [[ "$(git -C "$dir" rev-parse HEAD)" == "$LLAMA_COMMIT" ]] || { echo "llama.cpp $LLAMA_TAG: wrong commit" >&2; exit 1; }
fi
echo "$dir"
