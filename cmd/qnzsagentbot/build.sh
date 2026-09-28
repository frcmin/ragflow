#!/usr/bin/env bash
# Build only the QNZS agent-bot HTTP service.
# This package does not link the native DeepDoc libraries, so CGO stays off.
set -euo pipefail
cd "$(dirname "$0")/../.."
export CGO_ENABLED=0
go build -o bin/qnzsagentbot ./cmd/qnzsagentbot
echo "built bin/qnzsagentbot"
