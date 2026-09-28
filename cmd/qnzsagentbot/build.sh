#!/usr/bin/env bash
# Build the QNZS agentbot. This binary does not link DeepDoc, so CGO stays off.
set -euo pipefail
cd "$(dirname "$0")/../.."
export CGO_ENABLED=0
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
mkdir -p bin
go build -o bin/qnzsagentbot ./cmd/qnzsagentbot
echo "built bin/qnzsagentbot"
