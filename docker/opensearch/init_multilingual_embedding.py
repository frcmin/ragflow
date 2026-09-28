#!/usr/bin/env python3
"""Register and deploy a multilingual text-embedding model on OpenSearch.

Uses only the Python standard library so the Compose job can run on
python:3.13-alpine. See README.zh.md for the operator steps.
"""
import base64
import json
import os
import sys
import time
import urllib.error
import urllib.request

MODEL_GROUP = "ragflow_multilingual_embedding"
PIPELINE = "ragflow_multilingual_embedding"
INDEX = "ragflow_multilingual_knn_sample"
STATE_PATH = os.environ.get("OS_EMBEDDING_STATE_PATH", "/state/model_id")
TIMEOUT_SEC = int(os.environ.get("OS_EMBEDDING_TIMEOUT_SEC", "1200"))


def env(name, default=""):
    return os.environ.get(name, default)


def log(message):
    print(message, flush=True)


class OpenSearch:
    def __init__(self, base, user, password):
        self.base = base.rstrip("/")
        token = base64.b64encode(f"{user}:{password}".encode()).decode()
        self.auth = f"Basic {token}"

    def request(self, method, path, body=None, ok=(200, 201)):
        data = None
        headers = {"Authorization": self.auth}
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                raw = resp.read()
                status = resp.status
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            status = exc.code
        parsed = None
        if raw:
            try:
                parsed = json.loads(raw.decode())
            except json.JSONDecodeError:
                parsed = {"raw": raw.decode(errors="replace")}
        if status not in ok:
            raise RuntimeError(f"{method} {path} -> {status}: {parsed}")
        return status, parsed


def wait_ready(client):
    deadline = time.time() + 180
    while time.time() < deadline:
        try:
            _, health = client.request("GET", "/_cluster/health")
            status = (health or {}).get("status")
            if status in ("yellow", "green"):
                log(f"OpenSearch 集群状态: {status}")
                return
        except Exception as exc:
            log(f"等待 OpenSearch 就绪: {exc}")
        time.sleep(3)
    raise RuntimeError("OpenSearch 在 180 秒内未变为 yellow/green")


def put_settings(client):
    client.request(
        "PUT",
        "/_cluster/settings",
        {
            "persistent": {
                "plugins.ml_commons.only_run_on_ml_node": "false",
                "plugins.ml_commons.native_memory_threshold": "99",
                "plugins.ml_commons.allow_registering_model_via_url": "true",
                "plugins.ml_commons.model_access_control_enabled": "true",
                "plugins.ml_commons.model_auto_redeploy.enable": "true",
                "plugins.ml_commons.model_auto_redeploy.lifetime_retry_times": 3,
            }
        },
    )
    log("已写入 ML Commons 集群设置（含 allow_registering_model_via_url 与自动重新部署）")


def hits(payload):
    return ((payload or {}).get("hits") or {}).get("hits") or []


def find_by_name(client, path, name):
    _, payload = client.request(
        "POST",
        path,
        {"size": 20, "query": {"term": {"name.keyword": name}}},
    )
    found = hits(payload)
    if found:
        return found
    _, payload = client.request(
        "POST",
        path,
        {"size": 20, "query": {"match_phrase": {"name": name}}},
    )
    return [hit for hit in hits(payload) if (hit.get("_source") or {}).get("name") == name]


def ensure_model_group(client):
    found = find_by_name(client, "/_plugins/_ml/model_groups/_search", MODEL_GROUP)
    if found:
        group_id = found[0].get("_id") or (found[0].get("_source") or {}).get("model_group_id")
        log(f"复用模型组 {MODEL_GROUP}: {group_id}")
        return group_id
    _, created = client.request(
        "POST",
        "/_plugins/_ml/model_groups/_register",
        {"name": MODEL_GROUP, "description": "RAGFlow multilingual text embedding"},
    )
    group_id = created.get("model_group_id")
    log(f"已创建模型组 {MODEL_GROUP}: {group_id}")
    return group_id


def wait_task(client, task_id):
    deadline = time.time() + TIMEOUT_SEC
    while time.time() < deadline:
        _, task = client.request("GET", f"/_plugins/_ml/tasks/{task_id}")
        state = task.get("state")
        if state == "COMPLETED":
            return task
        if state in ("FAILED", "COMPLETED_WITH_ERROR"):
            raise RuntimeError(f"ML 任务 {task_id} 失败: {task}")
        log(f"等待任务 {task_id}: {state or 'UNKNOWN'}")
        time.sleep(5)
    raise RuntimeError(f"ML 任务 {task_id} 超时")


def ensure_model(client, group_id):
    name = env("OS_EMBEDDING_MODEL_NAME", "huggingface/sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2")
    version = env("OS_EMBEDDING_MODEL_VERSION", "1.0.2")
    model_format = env("OS_EMBEDDING_MODEL_FORMAT", "TORCH_SCRIPT")
    model_url = env("OS_EMBEDDING_MODEL_URL")
    found = find_by_name(client, "/_plugins/_ml/models/_search", name)
    for hit in found:
        source = hit.get("_source") or {}
        if source.get("model_state") == "DEPLOYED":
            model_id = hit.get("_id")
            log(f"模型已部署，复用 model_id={model_id}")
            return model_id
    if found:
        model_id = found[0].get("_id")
        log(f"模型已注册但未部署，开始部署 model_id={model_id}")
    else:
        body = {
            "name": name,
            "version": version,
            "model_group_id": group_id,
            "model_format": model_format,
        }
        if model_url:
            body["function_name"] = "TEXT_EMBEDDING"
            body["url"] = model_url
            if env("OS_EMBEDDING_MODEL_HASH"):
                body["model_content_hash_value"] = env("OS_EMBEDDING_MODEL_HASH")
            log(f"通过 URL 注册自定义模型 {name}")
        else:
            log(f"注册 OpenSearch 预训练模型 {name} {version}")
        _, created = client.request("POST", "/_plugins/_ml/models/_register", body)
        task = wait_task(client, created["task_id"])
        model_id = task.get("model_id")
        if not model_id:
            raise RuntimeError(f"注册完成但没有 model_id: {task}")
        log(f"注册完成 model_id={model_id}")
    _, deployed = client.request("POST", f"/_plugins/_ml/models/{model_id}/_deploy", {})
    task_id = deployed.get("task_id")
    if task_id:
        wait_task(client, task_id)
    log(f"部署完成 model_id={model_id}")
    return model_id


def ensure_pipeline_and_index(client, model_id, dimension):
    client.request(
        "PUT",
        f"/_ingest/pipeline/{PIPELINE}",
        {
            "description": "RAGFlow multilingual text embedding",
            "processors": [
                {
                    "text_embedding": {
                        "model_id": model_id,
                        "field_map": {"text": "text_embedding"},
                    }
                }
            ],
        },
    )
    log(f"已写入 ingest pipeline {PIPELINE}")
    try:
        client.request(
            "PUT",
            f"/{INDEX}",
            {
                "settings": {"index.knn": True, "default_pipeline": PIPELINE},
                "mappings": {
                    "properties": {
                        "text": {"type": "text"},
                        "text_embedding": {
                            "type": "knn_vector",
                            "dimension": dimension,
                            "method": {
                                "name": "hnsw",
                                "space_type": "cosinesimil",
                                "engine": "lucene",
                            },
                        },
                    }
                },
            },
        )
        log(f"已创建示例 k-NN 索引 {INDEX}，维度 {dimension}")
    except RuntimeError as exc:
        if "resource_already_exists_exception" in str(exc):
            log(f"示例索引 {INDEX} 已存在，保持原映射")
        else:
            raise


def verify(client, model_id, dimension):
    _, predicted = client.request(
        "POST",
        f"/_plugins/_ml/_predict/text_embedding/{model_id}",
        {
            "text_docs": ["你好，世界", "hello world"],
            "return_number": True,
            "target_response": ["sentence_embedding"],
        },
    )
    results = predicted.get("inference_results") or []
    if len(results) < 2:
        raise RuntimeError(f"_predict 返回条数异常: {predicted}")
    for item in results:
        output = (item.get("output") or [{}])[0]
        data = output.get("data") or []
        if len(data) != dimension:
            raise RuntimeError(f"向量维度是 {len(data)}，期望 {dimension}")
    log(f"_predict 校验通过：2 条文本，维度 {dimension}")

    for doc_id, text in (("hello", "hello world"), ("nihao", "你好，世界")):
        client.request("PUT", f"/{INDEX}/_doc/{doc_id}?refresh=true", {"text": text})
    _, searched = client.request(
        "POST",
        f"/{INDEX}/_search",
        {
            "size": 2,
            "_source": ["text"],
            "query": {
                "neural": {
                    "text_embedding": {
                        "query_text": "你好",
                        "model_id": model_id,
                        "k": 2,
                    }
                }
            },
        },
    )
    top = hits(searched)
    preview = [(hit.get("_id"), (hit.get("_source") or {}).get("text")) for hit in top]
    log(f"neural 查询「你好」命中: {preview}")


def write_state(model_id):
    directory = os.path.dirname(STATE_PATH)
    if directory:
        os.makedirs(directory, exist_ok=True)
    with open(STATE_PATH, "w", encoding="utf-8") as handle:
        handle.write(model_id + "\n")
    log(f"model_id 已写入 {STATE_PATH}")
    log(f"MODEL_ID={model_id}")


def main():
    base = env("OPENSEARCH_URL", "http://opensearch01:9201")
    user = env("OPENSEARCH_USER", "admin")
    password = env("OPENSEARCH_PASSWORD")
    if not password:
        log("缺少 OPENSEARCH_PASSWORD")
        return 1
    dimension = int(env("OS_EMBEDDING_DIMENSION", "384"))
    client = OpenSearch(base, user, password)
    wait_ready(client)
    put_settings(client)
    group_id = ensure_model_group(client)
    model_id = ensure_model(client, group_id)
    ensure_pipeline_and_index(client, model_id, dimension)
    verify(client, model_id, dimension)
    write_state(model_id)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:
        log(f"初始化失败: {exc}")
        sys.exit(1)
