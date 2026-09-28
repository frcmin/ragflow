#!/usr/bin/env bash
# Start the embedding HTTP server for a profile.
# bge-m3 is the local test profile. The Qwen3 profile is started with serve_qwen3.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
PROFILE="${1:-$ROOT/profiles/bge-m3.env}"
set -a
# shellcheck disable=SC1090
source "$PROFILE"
set +a
if [[ "${EMBEDDING_BACKEND:-}" == "llama-cpp-proxy" ]]; then
  echo "这个 profile 走 llama.cpp。请运行 serve_qwen3.sh，不要用本脚本。" >&2
  exit 2
fi
cd "$ROOT"
exec python3 "$ROOT/embedding_server.py"
