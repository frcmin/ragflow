#!/bin/sh
# Install the CPU PyTorch stack used to serve bge-m3, then start the OpenAI-compatible server.
set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends libgomp1
rm -rf /var/lib/apt/lists/*
pip install --no-cache-dir -r /opt/embedding/requirements-bge.txt
exec python /opt/embedding/embedding_server.py
