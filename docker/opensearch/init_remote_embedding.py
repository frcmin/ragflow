#!/usr/bin/env python3
"""Register a remote multilingual embedding model in OpenSearch ML Commons.

The model itself runs outside the cluster (sentence-transformers for bge-m3, llama.cpp for
quantized Qwen3-Embedding-4B). This script:

1. Allows the connector to call that private HTTP endpoint.
2. Creates an OpenAI-embeddings connector, registers it as a remote model, and deploys it.
3. Creates a text_embedding ingest pipeline and a sample k-NN index.
4. Indexes Chinese and English documents and runs neural queries.

Stdlib only, so the Compose job can use python:3.13-alpine.
"""
import base64
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import urlparse

MODEL_GROUP_DEFAULT = "ragflow_multilingual_embedding"
PUBLIC_TRUSTED_REGEX = [
    r"^https://runtime\.sagemaker\..*[a-z0-9-]\.amazonaws\.com/.*$",
    r"^https://api\.sagemaker\..*[a-z0-9-]\.amazonaws\.com/.*$",
    r"^https://api\.openai\.com/.*$",
    r"^https://api\.cohere\.ai/.*$",
    r"^https://bedrock-runtime\..*[a-z0-9-]\.amazonaws\.com/.*$",
    r"^https://bedrock-agent-runtime\..*[a-z0-9-]\.amazonaws\.com/.*$",
]
SAMPLE_DOCS = (
    ("zh-beijing", "北京是中国的首都，故宫和长城都在附近。"),
    ("zh-shanghai", "上海是一座港口城市，外滩在黄浦江边。"),
    ("en-paris", "Paris is the capital of France and is famous for the Eiffel Tower."),
    ("en-london", "London is the capital of the United Kingdom and sits on the Thames."),
)
# Same-language checks. The script also prints a cross-lingual query.
EXPECTED_TOP = (
    ("故宫在哪个城市", "zh-beijing"),
    ("Eiffel Tower", "en-paris"),
)
CROSS_LINGUAL_QUERY = "法国的首都"


def env(name, default=""):
    return os.environ.get(name, default)


def log(message):
    print(message, flush=True)


def trusted_regex_for(embedding_url):
    parsed = urlparse(embedding_url)
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise ValueError(f"EMBEDDING_URL 不是 http(s) 地址: {embedding_url}")
    host = re.escape(parsed.hostname)
    if parsed.port:
        return f"^{parsed.scheme}://{host}:{parsed.port}/.*$"
    return f"^{parsed.scheme}://{host}/.*$"


def health_url(embedding_url):
    parsed = urlparse(embedding_url)
    return f"{parsed.scheme}://{parsed.netloc}/health"


def connector_body(name, embedding_url, model_name, api_key):
    return {
        "name": name,
        "description": "RAGFlow OpenAI-compatible text embedding endpoint",
        "version": "1",
        "protocol": "http",
        "parameters": {"model": model_name},
        "credential": {"openAI_key": api_key},
        "client_config": {
            "max_connection": 30,
            "connection_timeout": 10000,
            "read_timeout": 180000,
        },
        "actions": [
            {
                "action_type": "predict",
                "method": "POST",
                "url": embedding_url,
                "headers": {
                    "Authorization": "Bearer ${credential.openAI_key}",
                    "Content-Type": "application/json",
                },
                "request_body": '{ "input": ${parameters.input}, "model": "${parameters.model}" }',
                "pre_process_function": "connector.pre_process.openai.embedding",
                "post_process_function": "connector.post_process.openai.embedding",
            }
        ],
    }


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
            with urllib.request.urlopen(req, timeout=300) as resp:
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


def hits(payload):
    return ((payload or {}).get("hits") or {}).get("hits") or []


def wait_http_ok(url, timeout):
    deadline = time.time() + timeout
    last = "not started"
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=10) as response:
                payload = json.loads(response.read().decode() or "{}")
            if response.status == 200 and payload.get("status") == "ok":
                return payload
            last = payload
        except Exception as exc:
            last = str(exc)
        log(f"等待向量服务 {url}: {last}")
        time.sleep(3)
    raise RuntimeError(f"向量服务未就绪: {last}")


def wait_ready(client, timeout):
    deadline = time.time() + timeout
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
    raise RuntimeError("OpenSearch 在限定时间内未变为 yellow/green")


def as_list(value):
    if value is None:
        return []
    if isinstance(value, list):
        return value
    if isinstance(value, str):
        return [value]
    return list(value)


def put_settings(client, embedding_url):
    regex = trusted_regex_for(embedding_url)
    _, current = client.request("GET", "/_cluster/settings?include_defaults=true&flat_settings=true")
    persistent = (current or {}).get("persistent") or {}
    defaults = (current or {}).get("defaults") or {}
    existing = as_list(
        persistent.get("plugins.ml_commons.trusted_connector_endpoints_regex")
        or defaults.get("plugins.ml_commons.trusted_connector_endpoints_regex")
    )
    if not existing:
        existing = list(PUBLIC_TRUSTED_REGEX)
    if regex not in existing:
        existing.append(regex)
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
                "plugins.ml_commons.connector.private_ip_enabled": True,
                "plugins.ml_commons.trusted_connector_endpoints_regex": existing,
            }
        },
    )
    log(f"已允许远程连接器访问 {regex}")


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
        {"size": 50, "query": {"match_phrase": {"name": name}}},
    )
    return [hit for hit in hits(payload) if (hit.get("_source") or {}).get("name") == name]


def ensure_model_group(client, name):
    found = find_by_name(client, "/_plugins/_ml/model_groups/_search", name)
    if found:
        group_id = found[0].get("_id")
        log(f"复用模型组 {name}: {group_id}")
        return group_id
    _, created = client.request(
        "POST",
        "/_plugins/_ml/model_groups/_register",
        {
            "name": name,
            "description": "RAGFlow remote multilingual text embedding",
            "access_mode": "public",
        },
    )
    group_id = created.get("model_group_id")
    log(f"已创建模型组 {name}: {group_id}")
    return group_id


def wait_task(client, task_id, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        _, task = client.request("GET", f"/_plugins/_ml/tasks/{task_id}")
        state = task.get("state")
        if state == "COMPLETED":
            return task
        if state in ("FAILED", "COMPLETED_WITH_ERROR"):
            raise RuntimeError(f"ML 任务 {task_id} 失败: {task}")
        log(f"等待任务 {task_id}: {state or 'UNKNOWN'}")
        time.sleep(2)
    raise RuntimeError(f"ML 任务 {task_id} 超时")


def connector_predict_url(payload):
    actions = (payload or {}).get("actions") or []
    if not actions:
        return ""
    return actions[0].get("url") or ""


def ensure_connector(client, name, embedding_url, model_name, api_key):
    body = connector_body(name, embedding_url, model_name, api_key)
    found = find_by_name(client, "/_plugins/_ml/connectors/_search", name)
    if not found:
        _, created = client.request("POST", "/_plugins/_ml/connectors/_create", body)
        connector_id = created.get("connector_id")
        log(f"已创建连接器 {name}: {connector_id}")
        return connector_id
    connector_id = found[0].get("_id")
    _, current = client.request("GET", f"/_plugins/_ml/connectors/{connector_id}")
    if connector_predict_url(current) == embedding_url and ((current or {}).get("parameters") or {}).get("model") == model_name:
        log(f"复用连接器 {name}: {connector_id}")
        return connector_id
    try:
        client.request("PUT", f"/_plugins/_ml/connectors/{connector_id}", body)
    except RuntimeError as exc:
        if "undeploy" not in str(exc):
            raise
        for model_id in _model_ids_from_undeploy_error(str(exc)):
            log(f"更新连接器前卸载模型 {model_id}")
            client.request("POST", f"/_plugins/_ml/models/{model_id}/_undeploy", {})
        client.request("PUT", f"/_plugins/_ml/connectors/{connector_id}", body)
    log(f"已更新连接器 {name}: {connector_id}")
    return connector_id


def _model_ids_from_undeploy_error(message):
    # "... please undeploy the models first: [id1, id2]"
    start = message.find("[")
    end = message.find("]", start + 1)
    if start < 0 or end < 0:
        return []
    return [item.strip() for item in message[start + 1 : end].split(",") if item.strip()]


def ensure_model(client, group_id, name, connector_id, timeout):
    found = find_by_name(client, "/_plugins/_ml/models/_search", name)
    model_id = None
    for hit in found:
        source = hit.get("_source") or {}
        if source.get("connector_id") == connector_id or not source.get("connector_id"):
            model_id = hit.get("_id")
            if source.get("model_state") == "DEPLOYED":
                log(f"模型已部署，复用 model_id={model_id}")
                return model_id
            break
    if model_id is None:
        _, created = client.request(
            "POST",
            "/_plugins/_ml/models/_register",
            {
                "name": name,
                "function_name": "remote",
                "model_group_id": group_id,
                "description": "Remote multilingual embedding for RAGFlow",
                "connector_id": connector_id,
            },
        )
        model_id = created.get("model_id")
        task_id = created.get("task_id")
        if task_id and not model_id:
            task = wait_task(client, task_id, timeout)
            model_id = task.get("model_id")
        if not model_id:
            raise RuntimeError(f"注册完成但没有 model_id: {created}")
        log(f"注册完成 model_id={model_id}")
    try:
        _, deployed = client.request("POST", f"/_plugins/_ml/models/{model_id}/_deploy", {})
    except RuntimeError as exc:
        if "deployed" not in str(exc).lower():
            raise
        log(f"模型已处于部署状态 model_id={model_id}")
        return model_id
    task_id = (deployed or {}).get("task_id")
    if task_id:
        wait_task(client, task_id, timeout)
    log(f"部署完成 model_id={model_id}")
    return model_id


def ensure_pipeline_and_index(client, pipeline, index, model_id, dimension):
    client.request(
        "PUT",
        f"/_ingest/pipeline/{pipeline}",
        {
            "description": "RAGFlow remote multilingual text embedding",
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
    log(f"已写入 ingest pipeline {pipeline}")
    try:
        _, existing = client.request("GET", f"/{index}/_mapping")
    except RuntimeError as exc:
        if "index_not_found_exception" not in str(exc):
            raise
        existing = None
    if existing:
        props = (((existing.get(index) or {}).get("mappings") or {}).get("properties") or {})
        current = ((props.get("text_embedding") or {}).get("dimension"))
        if current != dimension:
            raise RuntimeError(
                f"索引 {index} 的维度是 {current}，当前配置是 {dimension}。"
                "换模型或改 MRL 维度前先删除该索引。"
            )
        log(f"示例索引 {index} 已存在，维度 {dimension}")
        return
    client.request(
        "PUT",
        f"/{index}",
        {
            "settings": {
                "index.knn": True,
                "index.number_of_shards": 1,
                "index.number_of_replicas": 0,
                "default_pipeline": pipeline,
            },
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
    log(f"已创建示例 k-NN 索引 {index}，维度 {dimension}")


def index_samples(client, index):
    for doc_id, text in SAMPLE_DOCS:
        client.request("PUT", f"/{index}/_doc/{doc_id}?refresh=true", {"text": text})
        log(f"写入 {doc_id}: {text}")


def vector_length(predicted):
    """Lengths of every sentence_embedding tensor.

    A batched predict comes back as one inference result whose output array
    holds one tensor per input text.
    """
    results = (predicted or {}).get("inference_results") or []
    if not results:
        raise RuntimeError(f"_predict 没有 inference_results: {predicted}")
    lengths = []
    for item in results:
        for output in item.get("output") or []:
            if output.get("name") not in (None, "sentence_embedding"):
                continue
            lengths.append(len(output.get("data") or []))
    return lengths


def neural_search(client, index, model_id, query):
    _, searched = client.request(
        "POST",
        f"/{index}/_search",
        {
            "size": 4,
            "_source": ["text"],
            "query": {
                "neural": {
                    "text_embedding": {
                        "query_text": query,
                        "model_id": model_id,
                        "k": 4,
                    }
                }
            },
        },
    )
    found = []
    for hit in hits(searched):
        found.append(
            {
                "id": hit.get("_id"),
                "score": hit.get("_score"),
                "text": (hit.get("_source") or {}).get("text"),
            }
        )
    return found


def verify(client, index, model_id, dimension):
    _, predicted = client.request(
        "POST",
        f"/_plugins/_ml/_predict/text_embedding/{model_id}",
        {
            "text_docs": ["你好，世界", "hello world"],
            "return_number": True,
            "target_response": ["sentence_embedding"],
        },
    )
    lengths = vector_length(predicted)
    if lengths != [dimension, dimension]:
        raise RuntimeError(f"_predict 维度 {lengths}，期望两条 {dimension}")
    log(f"_predict 校验通过：2 条文本，维度 {dimension}")
    index_samples(client, index)
    for query, expected in EXPECTED_TOP:
        found = neural_search(client, index, model_id, query)
        log(f"NEURAL query={query}")
        for rank, hit in enumerate(found, start=1):
            log(f"  {rank}. {hit['id']} score={hit['score']} text={hit['text']}")
        if not found or found[0]["id"] != expected:
            top = found[0]["id"] if found else None
            raise RuntimeError(f"查询「{query}」的第一名是 {top}，期望 {expected}")
    cross = neural_search(client, index, model_id, CROSS_LINGUAL_QUERY)
    log(f"NEURAL query={CROSS_LINGUAL_QUERY} (cross-lingual, printed for inspection)")
    for rank, hit in enumerate(cross, start=1):
        log(f"  {rank}. {hit['id']} score={hit['score']} text={hit['text']}")


def write_state(model_id, profile, dimension, embedding_url):
    path = env("OS_EMBEDDING_STATE_PATH", "/state/model_id")
    directory = os.path.dirname(path)
    if directory:
        os.makedirs(directory, exist_ok=True)
    content = (
        f"MODEL_ID={model_id}\nPROFILE={profile}\nDIMENSION={dimension}\nEMBEDDING_URL={embedding_url}\n"
    )
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(content)
    log(f"model_id 已写入 {path}")
    log(f"MODEL_ID={model_id}")


def main():
    base = env("OPENSEARCH_URL", "http://opensearch01:9201")
    user = env("OPENSEARCH_USER", "admin")
    password = env("OPENSEARCH_PASSWORD")
    embedding_url = env("EMBEDDING_URL")
    model_name = env("EMBEDDING_MODEL_NAME")
    if not password or not embedding_url or not model_name:
        log("需要 OPENSEARCH_PASSWORD、EMBEDDING_URL、EMBEDDING_MODEL_NAME")
        return 1
    dimension = int(env("EMBEDDING_DIMENSION", "1024"))
    profile = env("EMBEDDING_PROFILE", model_name)
    connector_name = env("EMBEDDING_CONNECTOR_NAME", "ragflow_remote_embedding")
    pipeline = env("EMBEDDING_PIPELINE", "ragflow_remote_embedding")
    index = env("EMBEDDING_INDEX", "ragflow_remote_embedding_sample")
    group_name = env("EMBEDDING_MODEL_GROUP", MODEL_GROUP_DEFAULT)
    api_key = env("EMBEDDING_API_KEY", "local-embedding")
    timeout = int(env("OS_EMBEDDING_TIMEOUT_SEC", "1800"))
    client = OpenSearch(base, user, password)
    wait_ready(client, int(env("OPENSEARCH_WAIT_SEC", "180")))
    health = wait_http_ok(health_url(embedding_url), timeout)
    reported = health.get("dimension")
    if reported is not None and int(reported) != dimension:
        raise RuntimeError(f"向量服务维度 {reported} 与 EMBEDDING_DIMENSION {dimension} 不一致")
    log(f"向量服务就绪: {health}")
    put_settings(client, embedding_url)
    group_id = ensure_model_group(client, group_name)
    connector_id = ensure_connector(client, connector_name, embedding_url, model_name, api_key)
    model_id = ensure_model(client, group_id, model_name, connector_id, timeout)
    ensure_pipeline_and_index(client, pipeline, index, model_id, dimension)
    verify(client, index, model_id, dimension)
    write_state(model_id, profile, dimension, embedding_url)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:
        log(f"初始化失败: {exc}")
        sys.exit(1)
