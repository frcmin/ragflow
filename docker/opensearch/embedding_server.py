#!/usr/bin/env python3
"""OpenAI-compatible embedding HTTP server.

The OpenSearch ML Commons connector always calls POST /v1/embeddings.
Two backends share that contract:

- sentence-transformers: local model (the bge-m3 test path)
- llama-cpp-proxy: forward to llama-server, then L2-normalize and optionally
  truncate with Matryoshka (MRL). Used for quantized Qwen3-Embedding-4B.

The process exits only after the local model is loaded. /health stays 503
until then, so the OpenSearch init job can wait.
"""
import json
import math
import os
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def env(name, default=""):
    return os.environ.get(name, default)


def env_bool(name, default=False):
    raw = os.environ.get(name)
    if raw is None or raw == "":
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def l2_normalize(values):
    norm = math.sqrt(sum(value * value for value in values))
    if norm == 0.0:
        return list(values)
    return [value / norm for value in values]


def fit_dimension(values, dimension, native_dimension, mrl):
    """Return an L2-normalized vector of `dimension`.

    When `dimension` is smaller than the model width, MRL keeps the leading
    coordinates and renormalizes. That is the Qwen3-Embedding contract.
    """
    vector = [float(value) for value in values]
    width = len(vector)
    if dimension <= 0:
        raise ValueError("EMBEDDING_DIMENSION must be positive")
    if width == dimension:
        return l2_normalize(vector)
    if native_dimension and width != native_dimension and width != dimension:
        raise ValueError(f"upstream width {width} is neither {native_dimension} nor {dimension}")
    if dimension > width:
        raise ValueError(f"requested dimension {dimension} is larger than vector width {width}")
    if not mrl:
        raise ValueError(
            f"vector width {width} != EMBEDDING_DIMENSION {dimension}; set EMBEDDING_MRL=true to truncate"
        )
    return l2_normalize(vector[:dimension])


def append_eos(texts, eos):
    if not eos:
        return list(texts)
    return [text + eos for text in texts]


def openai_embeddings(vectors, model):
    return {
        "object": "list",
        "data": [
            {"object": "embedding", "index": index, "embedding": vector}
            for index, vector in enumerate(vectors)
        ],
        "model": model,
        "usage": {"prompt_tokens": 0, "total_tokens": 0},
    }


def extract_vectors(payload):
    if not isinstance(payload, dict):
        raise ValueError("embedding response is not an object")
    rows = payload.get("data")
    if isinstance(rows, list) and rows:
        ordered = sorted(rows, key=lambda row: row.get("index", 0))
        return [row["embedding"] for row in ordered]
    embedding = payload.get("embedding")
    if isinstance(embedding, list) and embedding:
        if embedding and isinstance(embedding[0], list):
            return embedding
        return [embedding]
    raise ValueError(f"no embeddings in response keys {list(payload)}")


def coerce_inputs(payload):
    if not isinstance(payload, dict) or "input" not in payload:
        raise ValueError("request body must be a JSON object with input")
    raw = payload["input"]
    if isinstance(raw, str):
        texts = [raw]
    elif isinstance(raw, list) and all(isinstance(item, str) for item in raw):
        texts = raw
    else:
        raise ValueError("input must be a string or a list of strings")
    if not texts:
        raise ValueError("input is empty")
    return texts


class EmbeddingService:
    def __init__(self):
        self.backend = env("EMBEDDING_BACKEND", "sentence-transformers")
        self.model_name = env("EMBEDDING_MODEL_NAME", "BAAI/bge-m3")
        self.dimension = int(env("EMBEDDING_DIMENSION", "1024"))
        self.native_dimension = int(env("EMBEDDING_NATIVE_DIMENSION", str(self.dimension)))
        self.mrl = env_bool("EMBEDDING_MRL", False)
        self.eos = env("EMBEDDING_APPEND_EOS", "")
        self.upstream = env("EMBEDDING_UPSTREAM_URL", "").rstrip("/")
        self._model = None
        self._ready = False
        self._error = ""
        self._lock = threading.Lock()

    def load(self):
        if self.backend == "llama-cpp-proxy":
            if not self.upstream:
                raise RuntimeError("EMBEDDING_UPSTREAM_URL is required for llama-cpp-proxy")
            self._ready = True
            return
        if self.backend != "sentence-transformers":
            raise RuntimeError(f"unsupported EMBEDDING_BACKEND {self.backend}")
        from sentence_transformers import SentenceTransformer

        self._model = SentenceTransformer(self.model_name, device="cpu")
        max_length = int(env("EMBEDDING_MAX_LENGTH", "512"))
        if max_length > 0:
            self._model.max_seq_length = max_length
        # Load weights before serving so /health means the model can embed.
        probe = self._embed_local(["healthcheck"])
        if len(probe[0]) != self.dimension:
            raise RuntimeError(f"model width after fit is {len(probe[0])}, expected {self.dimension}")
        self._ready = True

    def health(self):
        if self.backend == "llama-cpp-proxy":
            ok, detail = self._upstream_health()
            if not ok:
                return 503, {"status": "loading", "backend": self.backend, "detail": detail}
        if not self._ready:
            return 503, {"status": "loading", "backend": self.backend, "detail": self._error}
        return 200, {
            "status": "ok",
            "backend": self.backend,
            "model": self.model_name,
            "dimension": self.dimension,
            "native_dimension": self.native_dimension,
            "mrl": self.mrl,
        }

    def embed(self, texts):
        texts = append_eos(texts, self.eos)
        if self.backend == "llama-cpp-proxy":
            vectors = self._embed_upstream(texts)
        else:
            with self._lock:
                vectors = self._embed_local(texts)
        return [fit_dimension(vector, self.dimension, self.native_dimension, self.mrl) for vector in vectors]

    def _embed_local(self, texts):
        encoded = self._model.encode(texts, normalize_embeddings=False, show_progress_bar=False)
        rows = [[float(value) for value in vector] for vector in encoded]
        if len(rows) != len(texts):
            raise RuntimeError(f"sentence-transformers returned {len(rows)} vectors for {len(texts)} inputs")
        return rows

    def _embed_upstream(self, texts):
        body = json.dumps({"input": texts, "model": self.model_name}).encode()
        request = urllib.request.Request(
            self.upstream,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=300) as response:
                payload = json.loads(response.read().decode())
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise RuntimeError(f"upstream embeddings failed: {exc.code} {detail}") from exc
        vectors = extract_vectors(payload)
        if len(vectors) != len(texts):
            raise RuntimeError(f"upstream returned {len(vectors)} vectors for {len(texts)} inputs")
        return vectors

    def _upstream_health(self):
        # EMBEDDING_UPSTREAM_URL ends with /v1/embeddings; llama-server health is at the origin.
        origin = self.upstream.split("/v1/")[0] + "/health"
        request = urllib.request.Request(origin, method="GET")
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                if response.status == 200:
                    return True, "ok"
                return False, f"status {response.status}"
        except Exception as exc:
            return False, str(exc)


SERVICE = EmbeddingService()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        print(f"{self.address_string()} {fmt % args}", flush=True)

    def do_GET(self):
        if self.path.split("?", 1)[0] != "/health":
            self._send(404, {"error": "not found"})
            return
        status, payload = SERVICE.health()
        self._send(status, payload)

    def do_POST(self):
        if self.path.split("?", 1)[0] != "/v1/embeddings":
            self._send(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        try:
            texts = coerce_inputs(json.loads(raw.decode() or "{}"))
            vectors = SERVICE.embed(texts)
            self._send(200, openai_embeddings(vectors, SERVICE.model_name))
        except Exception as exc:
            self._send(400, {"error": str(exc)})

    def _send(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    host = env("EMBEDDING_HOST", "0.0.0.0")
    port = int(env("EMBEDDING_PORT", "8080"))
    try:
        SERVICE.load()
    except Exception as exc:
        SERVICE._error = str(exc)
        print(f"模型加载失败: {exc}", flush=True)
        raise
    httpd = ThreadingHTTPServer((host, port), Handler)
    print(
        f"embedding server {SERVICE.backend} model={SERVICE.model_name} "
        f"dim={SERVICE.dimension} native={SERVICE.native_dimension} mrl={SERVICE.mrl} on {host}:{port}",
        flush=True,
    )
    httpd.serve_forever()


if __name__ == "__main__":
    main()
