# OpenSearch 多语言向量模型

向量不在 OpenSearch 进程里算。模型跑在集群外面，OpenSearch 用 ML Commons 的 remote connector 注册成一个 `model_id`。ingest pipeline 的 `text_embedding` 和 neural 查询都只认这个 `model_id`。

测试和生产资料是同一套注册脚本 `init_remote_embedding.py`。差别只在 profile：模型、地址、维度、要不要做 MRL 截断。

## 为什么不把 Qwen3 装进 OpenSearch 进程

生产模型是量化版 **Qwen3-Embedding-4B**。官方权重是 GGUF（`Qwen/Qwen3-Embedding-4B-GGUF`）。ML Commons 在节点内只能加载 TORCH_SCRIPT / ONNX，不能加载 GGUF。把 4B 模型转成 TorchScript 再塞进单个 OpenSearch 进程，native 内存会压过当前单节点的堆外预算。

官方模型卡给出的用法是 llama.cpp：`llama-server -m model.gguf --embedding --pooling last`。所以生产路径是：

1. llama.cpp 加载 GGUF，提供 OpenAI 兼容的 `POST /v1/embeddings`。
2. 前面加一层很薄的代理：补上 Qwen 要求的 `<|endoftext|>`，做 L2 归一化，并在打开 MRL 时截断维度。
3. OpenSearch 连接器使用内置的 `connector.pre_process.openai.embedding` / `connector.post_process.openai.embedding`，把这个 HTTP 服务登记成 remote 模型。

vLLM、TEI、Ollama 如果也提供同样的 `/v1/embeddings` 响应（`data[].embedding`），把 `EMBEDDING_UPSTREAM_URL` 指过去即可，不用改 OpenSearch 注册脚本。默认不用它们：vLLM / TEI 更适合 GPU 上的非 GGUF 权重；Ollama 多一层打包，而官方说明写的是 llama.cpp。

## 两个 profile

| | 测试 `profiles/bge-m3.env` | 生产 `profiles/qwen3-embedding-4b.env` |
|---|---|---|
| 模型 | `BAAI/bge-m3` | `Qwen/Qwen3-Embedding-4B` |
| 怎么跑 | sentence-transformers（CPU） | llama.cpp + 官方 GGUF |
| 维度 | 1024，不截断 | 默认 2560，不截断 |
| 量化 | 官方 BAAI/bge-m3 权重 | 默认 `Q4_K_M`，可改 `Q8_0` |
| 示例索引 | `ragflow_bge_m3_sample` | `ragflow_qwen3_embedding_4b_sample` |

bge-m3 和 Qwen3 的向量不是同一个空间。知识库索引不会被这个任务改写。换模型必须按新维度重建索引并重新嵌入。

## Qwen3-Embedding-4B

- 仓库：`Qwen/Qwen3-Embedding-4B-GGUF`
- 原生维度：**2560**。支持 MRL，可以把向量截成 32 到 2560 之间的任意长度，截的是前面的坐标，然后再做 L2 归一化。
- 默认 **不截断**（`EMBEDDING_DIMENSION=2560`，`EMBEDDING_MRL=false`）。索引更准，也更大。
- 要换成 1024 维：在 profile 里设 `EMBEDDING_DIMENSION=1024` 和 `EMBEDDING_MRL=true`，删掉旧的示例索引后再注册。1024 是省内存的折中，不是默认。
- 语言：100 多种，包含中文和英文。
- 量化（文件大小以 Hugging Face 上的 `Content-Length` 为准）：

| 量化 | 文件 | 大小 | 向量进程建议内存 | 和 OpenSearch 同机时的整机建议 |
|---|---|---|---|---|
| `Q4_K_M`（默认） | `Qwen3-Embedding-4B-Q4_K_M.gguf` | 2496703776 字节（约 2.33 GiB） | 8 GB | 16 GB |
| `Q8_0` | `Qwen3-Embedding-4B-Q8_0.gguf` | 4279660224 字节（约 3.99 GiB） | 12 GB | 24 GB |

llama.cpp 的上下文默认 `-c 8192`，而不是模型卡上的 32k，避免 KV cache 把内存吃满。短文本嵌入够用。加大上下文会多占内存。CPU 即可，4 核以上更合适。有 GPU 时可以给 `llama-server` 加 `-ngl`，脚本默认不加。

Qwen 建议检索 query 侧加英文 instruction，passage 侧不加。OpenSearch 的 neural 查询和入库走的是同一个 `model_id`，代理分不清哪一次是 query。因此代理 **不会** 自动加 instruction。需要的话，在查询文本里自己加，例如：

```text
Instruct: Given a web search query, retrieve relevant passages that answer the query
Query: 法国的首都
```

文档字段保持原文。

换 `Q8_0` 时只改这三行，维度仍然是 2560：

```bash
EMBEDDING_GGUF_FILE=Qwen3-Embedding-4B-Q8_0.gguf
EMBEDDING_GGUF_SIZE=4279660224
EMBEDDING_QUANT=Q8_0
```

## 用 Compose 跑

在 `docker/` 目录。两种 profile 不要同时开，它们都占用宿主机的 `18080`。

测试（bge-m3）：

```bash
docker compose -f docker-compose-base.yml --profile opensearch --profile embedding-bge up -d
docker compose -f docker-compose-base.yml --profile embedding-bge logs -f opensearch-embedding-init-bge
```

生产（Qwen3-Embedding-4B，Q4_K_M）。这一步会下载约 2.33 GiB 的 GGUF 并加载模型，小内存机器不要跑：

```bash
docker compose -f docker-compose-base.yml --profile opensearch --profile embedding-qwen3 up -d
docker compose -f docker-compose-base.yml --profile embedding-qwen3 logs -f opensearch-embedding-init-qwen3
```

`llama-server` 镜像默认是 `ghcr.io/ggml-org/llama.cpp:server`（CPU）。要钉死版本或改用 CUDA 镜像时设置 `LLAMA_CPP_IMAGE`。私有 Hugging Face 用 `HF_TOKEN`。

初始化任务会：

1. 等 OpenSearch 变为 yellow/green，并等向量服务的 `/health`。
2. 打开 `plugins.ml_commons.connector.private_ip_enabled`，并把向量服务的地址加入 `trusted_connector_endpoints_regex`（保留已有的公网白名单）。
3. 创建或更新连接器，注册 `function_name=remote` 的模型并部署。
4. 写入 ingest pipeline，创建示例 k-NN 索引（HNSW、cosine、Lucene）。已有索引的维度不一致时会直接失败，不会悄悄改映射。
5. 写入中英样例文档，跑 `_predict` 和 neural 查询。
6. 把 `MODEL_ID=` 打到日志，并写入卷 `os_embedding_state` 的 `/state/model_id`。

OpenSearch 堆仍然是 `-Xms2g -Xmx2g`。4B 模型不在这个 JVM 里。

## 不用 Compose，在主机上跑

测试：

```bash
cd docker/opensearch
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements-bge.txt
bash serve_embedding.sh profiles/bge-m3.env
```

另一个终端，OpenSearch 已经在听 `http://127.0.0.1:9201` 时：

```bash
set -a
source docker/opensearch/profiles/bge-m3.env
set +a
export OPENSEARCH_URL=http://127.0.0.1:9201
export OPENSEARCH_PASSWORD=infini_rag_flow_OS_01
python3 docker/opensearch/init_remote_embedding.py
```

生产（会下载并加载 Qwen3，确认机器内存后再执行）：

```bash
# 需要 PATH 里有 llama.cpp 的 llama-server，或设置 LLAMA_SERVER
bash docker/opensearch/serve_qwen3.sh
# 另一个终端 source profiles/qwen3-embedding-4b.env 后跑 init_remote_embedding.py
```

`serve_qwen3.sh` 会调用 `download_gguf.py`。目标文件大小已经对上就跳过下载。

## 手动验证

下面用测试 profile 的索引名。生产把索引名换成 `ragflow_qwen3_embedding_4b_sample`，维度是 2560（或你设的 MRL 维度）。`OS_PORT` 默认 1201。`MODEL_ID` 用日志里的值。

```bash
curl -s -u "admin:${OPENSEARCH_PASSWORD}" \
  -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${OS_PORT}/_plugins/_ml/_predict/text_embedding/MODEL_ID" \
  -d '{"text_docs":["你好，世界","hello world"],"target_response":["sentence_embedding"]}'

curl -s -u "admin:${OPENSEARCH_PASSWORD}" \
  -H 'Content-Type: application/json' \
  -X POST "http://127.0.0.1:${OS_PORT}/ragflow_bge_m3_sample/_search" \
  -d '{"_source":["text"],"query":{"neural":{"text_embedding":{"query_text":"故宫在哪个城市","model_id":"MODEL_ID","k":4}}}}'
```

直接打向量服务：

```bash
curl -s http://127.0.0.1:18080/health
curl -s http://127.0.0.1:18080/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"input":["你好，世界","hello world"]}'
```

## Helm

Helm 只把 OpenSearch 镜像设为 3.8.0，并打开 `connector.private_ip_enabled`。它不会启动 llama.cpp，也不会注册模型。集群起来之后，单独跑 `init_remote_embedding.py`，把 `EMBEDDING_URL` 指到集群能访问的向量服务。
