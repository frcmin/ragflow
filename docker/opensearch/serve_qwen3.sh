#!/usr/bin/env bash
# Production path: download the quantized GGUF, start llama-server, then the
# OpenAI-compatible proxy that normalizes and optionally applies MRL.
# Do not run this on a machine that cannot hold Qwen3-Embedding-4B.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
PROFILE="${1:-$ROOT/profiles/qwen3-embedding-4b.env}"
set -a
# shellcheck disable=SC1090
source "$PROFILE"
set +a

export GGUF_REPO="${EMBEDDING_GGUF_REPO}"
export GGUF_FILE="${EMBEDDING_GGUF_FILE}"
export GGUF_SIZE="${EMBEDDING_GGUF_SIZE}"
export GGUF_DEST="${EMBEDDING_GGUF_DEST:-$ROOT/models/$EMBEDDING_GGUF_FILE}"
python3 "$ROOT/download_gguf.py"

LLAMA_SERVER="${LLAMA_SERVER:-llama-server}"
LLAMA_PORT="${LLAMA_PORT:-8081}"
if ! command -v "$LLAMA_SERVER" >/dev/null 2>&1; then
  echo "找不到 $LLAMA_SERVER。安装官方 llama.cpp 的 llama-server，或设置 LLAMA_SERVER=/path/to/llama-server。" >&2
  echo "官方模型卡的启动方式: llama-server -m <gguf> --embedding --pooling last -ub 8192" >&2
  exit 1
fi

"$LLAMA_SERVER" \
  -m "$GGUF_DEST" \
  --embedding \
  --pooling last \
  --host 127.0.0.1 \
  --port "$LLAMA_PORT" \
  -c "${LLAMA_CTX:-8192}" \
  -ub 8192 \
  -t "${LLAMA_THREADS:-4}" &
llama_pid=$!
trap 'kill "$llama_pid" 2>/dev/null || true' EXIT

export EMBEDDING_BACKEND=llama-cpp-proxy
export EMBEDDING_UPSTREAM_URL="http://127.0.0.1:${LLAMA_PORT}/v1/embeddings"
export EMBEDDING_HOST="${EMBEDDING_HOST:-0.0.0.0}"
export EMBEDDING_PORT="${EMBEDDING_PORT:-18080}"
exec python3 "$ROOT/embedding_server.py"
