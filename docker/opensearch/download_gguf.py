#!/usr/bin/env python3
"""Download one GGUF file from Hugging Face.

Used by the Qwen3-Embedding-4B production path. Skips the download when the
destination already has the expected size. Stdlib only.
"""
import os
import sys
import urllib.request


def env(name, default=""):
    return os.environ.get(name, default)


def log(message):
    print(message, flush=True)


def destination_ready(path, expected):
    if not path or not os.path.isfile(path):
        return False
    size = os.path.getsize(path)
    if expected and size != expected:
        log(f"已有文件大小 {size}，期望 {expected}，重新下载")
        return False
    if not expected and size <= 0:
        return False
    log(f"已存在 {path} ({size} bytes)，跳过下载")
    return True


def download(url, dest, token):
    parent = os.path.dirname(dest)
    if parent:
        os.makedirs(parent, exist_ok=True)
    temporary = dest + ".partial"
    headers = {"User-Agent": "ragflow-opensearch-embedding"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(url, headers=headers)
    log(f"下载 {url}")
    with urllib.request.urlopen(request, timeout=120) as response, open(temporary, "wb") as handle:
        total = 0
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            handle.write(chunk)
            total += len(chunk)
            if total % (256 * 1024 * 1024) < 1024 * 1024:
                log(f"已写入 {total} bytes")
    os.replace(temporary, dest)
    log(f"下载完成 {dest} ({os.path.getsize(dest)} bytes)")


def main():
    repo = env("GGUF_REPO", "Qwen/Qwen3-Embedding-4B-GGUF")
    filename = env("GGUF_FILE", "Qwen3-Embedding-4B-Q4_K_M.gguf")
    dest = env("GGUF_DEST", os.path.join("models", filename))
    raw_size = env("GGUF_SIZE", "")
    expected = int(raw_size) if raw_size else 0
    token = env("HF_TOKEN") or env("HUGGING_FACE_HUB_TOKEN")
    if destination_ready(dest, expected):
        return 0
    url = f"https://huggingface.co/{repo}/resolve/main/{filename}"
    try:
        download(url, dest, token)
    except Exception as exc:
        log(f"下载失败: {exc}")
        return 1
    if expected and os.path.getsize(dest) != expected:
        log(f"大小不符: {os.path.getsize(dest)} != {expected}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
