# OpenSearch 多语言向量模型

本目录的初始化任务会在 OpenSearch 里用 ML Commons 注册并部署一个多语言文本向量模型，供 neural search / ingest pipeline 使用。

## 选型

默认模型是 OpenSearch 官方预训练模型 `huggingface/sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`（版本 `1.0.2`）。

- 向量维度：**384**
- 语言：含中文和英文在内的多语言句子向量（sentence-transformers multilingual MiniLM）
- 选择原因：它在 OpenSearch 预训练模型列表里，Compose 任务可以直接按名称注册 TorchScript 包，不需要自己转换模型。单节点、CPU、约 8GB 容器内存可以放下。`distiluse-base-multilingual-cased-v1`（512 维）也在官方列表里，但 MiniLM-L12 更常见，内存更小。
- 没有选用 bge-m3 / multilingual-e5：它们不在 OpenSearch 预训练仓库里，需要先转成 TorchScript 或 ONNX 再通过 URL 上传。可以换，但不是开箱即用。

## 怎么跑

在仓库的 `docker/` 目录：

```bash
# .env 里 DOC_ENGINE=opensearch，或显式打开 profile
docker compose -f docker-compose-base.yml --profile opensearch up -d
```

`opensearch-embedding-init` 会等 `opensearch01` 健康后执行 `init_multilingual_embedding.py`。它会：

1. 写入 ML Commons 集群设置（`only_run_on_ml_node=false`、`native_memory_threshold=99`、`allow_registering_model_via_url=true`、访问控制、节点重启后自动重新部署）。
2. 创建模型组 `ragflow_multilingual_embedding`（已存在则复用）。
3. 注册并部署模型，已部署则直接复用。
4. 创建 ingest pipeline `ragflow_multilingual_embedding`（`text` → `text_embedding`）。
5. 创建示例 k-NN 索引 `ragflow_multilingual_knn_sample`（维度 384，HNSW + cosine + Lucene）。
6. 用「你好，世界」和「hello world」做 `_predict`，并跑一条 neural 查询。
7. 把 `model_id` 打到日志，并写入卷 `os_embedding_state` 的 `/state/model_id`。

查看结果：

```bash
docker compose -f docker-compose-base.yml --profile opensearch logs opensearch-embedding-init
```

日志末尾有 `MODEL_ID=`。

容器内存建议不少于 8GB（`.env` 的 `MEM_LIMIT`）。JVM 堆默认 `-Xms2g -Xmx2g`，把其余内存留给模型的 native 内存。模型文件从 OpenSearch 的模型仓库下载，节点需要能访问外网。

## 手动验证

下面假设宿主机端口是 `.env` 里的 `OS_PORT`（默认 1201），密码是 `OPENSEARCH_PASSWORD`。把 `MODEL_ID` 换成日志里的值。

预测（应返回两条长度为 384 的向量）：

```bash
curl -s -u "admin:${OPENSEARCH_PASSWORD}" \
  -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${OS_PORT}/_plugins/_ml/_predict/text_embedding/MODEL_ID" \
  -d '{"text_docs":["你好，世界","hello world"],"target_response":["sentence_embedding"]}'
```

neural 查询：

```bash
curl -s -u "admin:${OPENSEARCH_PASSWORD}" \
  -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${OS_PORT}/ragflow_multilingual_knn_sample/_search" \
  -d '{"_source":["text"],"query":{"neural":{"text_embedding":{"query_text":"你好","model_id":"MODEL_ID","k":2}}}}'
```

## 换成更强的模型

bge-m3 一类模型需要你自己准备 OpenSearch 能加载的 TorchScript（或支持的 ONNX）文件，并放到 OpenSearch 节点能下载的 HTTP(S) 地址。然后在 `.env` 里改：

```bash
OS_EMBEDDING_MODEL_NAME=BAAI/bge-m3
OS_EMBEDDING_MODEL_VERSION=1.0.0
OS_EMBEDDING_DIMENSION=1024
OS_EMBEDDING_MODEL_FORMAT=TORCH_SCRIPT
OS_EMBEDDING_MODEL_URL=https://example.internal/bge-m3-torchscript.zip
OS_EMBEDDING_MODEL_HASH=   # 可选，OpenSearch 要求时再填
```

删掉示例索引后再跑初始化任务（已有索引不会被改维度）：

```bash
curl -u "admin:${OPENSEARCH_PASSWORD}" -X DELETE \
  "http://127.0.0.1:${OS_PORT}/ragflow_multilingual_knn_sample"
docker compose -f docker-compose-base.yml --profile opensearch up opensearch-embedding-init
```

用这个模型写出的向量和 RAGFlow 知识库索引里已有的向量不是同一套空间。知识库若要改用它，需要按新维度重建索引并重新嵌入文档。这个初始化任务只负责在 OpenSearch 里把模型部署好，并留下 pipeline 和示例索引。
