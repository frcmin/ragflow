# QNZS Agent Bot（Go）

这个进程只提供一个接口：

`POST /agentbots/{agent_id}/chat/completions`

同时监听 `POST /api/v1/agentbots/{agent_id}/chat/completions`，方便接到 RAGFlow 现有的 `/api/v1` 前缀后面。

它对应自定义 Python 里的 `qnzs_agent_bot_completions` / `qnzs_completion`：Bearer 鉴权、session 拼接、Begin 入参映射、OpenAI 风格的 SSE。画布真正执行时，Go 调用本仓库里的 Python Canvas 运行时（`POST /internal/qnzs/canvas/run`），不会在 Go 里再实现一套组件。

## 为什么执行留在 Python

仓库里已经有一套 Go agent/canvas 运行时，但它和检索、LLM、工具、存储绑在整个 `ragflow_server` 上，而且默认构建要链 DeepDoc 的 CGO 库。这个接口要单独编译、单独跑。Go 负责鉴权、`user_canvas` 归属、session、Begin 入参和 SSE；Python 负责 `Canvas.reset()`、把 `dialog` 写成 history、再 `canvas.run()`。内部接口用 `QNZS_INTERNAL_TOKEN` 保护，没有这个环境变量时 Python 会拒绝请求。

这个 fork 里没有 `QNZSUserCanvasService` 这张单独的表。Go 和内部 Python 都读现有的 `user_canvas`，并用 `user_id == tenant_id` 做归属校验，和参考实现一致。

## 构建和启动

需要 Go 1.27（与 `go.mod` 一致）。这个二进制不需要 CGO。

```bash
bash cmd/qnzsagentbot/build.sh
export MYSQL_HOST=127.0.0.1
export MYSQL_PORT=3306
export MYSQL_USER=root
export MYSQL_PASSWORD=infini_rag_flow
export MYSQL_DBNAME=rag_flow
export QNZS_INTERNAL_TOKEN=change-me
export QNZS_CANVAS_EXEC_URL=http://127.0.0.1:9380/internal/qnzs/canvas/run
export QNZS_LISTEN=:9386
./bin/qnzsagentbot
```

Python API 进程也要设置同一个 `QNZS_INTERNAL_TOKEN`，否则内部执行接口返回 503。

## 请求行为

- `Authorization` 必须是两段（`Bearer <secret>`）。先按 `api_token.beta` 查，没有再按 `api_token.token` 查。失败时 HTTP 200，正文 `code` 为 102，文案与参考实现相同（含末尾引号）。`tenant_id` 来自命中的那一行。
- `session_id` 取正文，否则取 `Session-Id` 头，再否则取正文里的 `Session-Id`。然后拼接成 `{session_id}-{agent_id}`。
- 从 Begin 组件的 inputs 组 `inputs`。键名里的 `a.b` 和 `a__b` 会到正文的嵌套对象里取值；键名就是 `dialog` 时值为空字符串。正文里的 `dialog` 数组会作为画布 history。
- `stream` 缺省为 true。流式响应头：`Cache-Control: no-cache`、`Connection: keep-alive`、`X-Accel-Buffering: no`、`Content-Type: text/event-stream; charset=utf-8`。每帧是 `chat.completion.chunk`，`model` 为 `agent-bot`。
- `stream` 为 false 时返回一条聚合后的 `chat.completion`（包在 `{"code":0,"data":...}` 里），而不是参考实现里只返回第一帧的写法。

## 调用示例

```bash
curl -N http://127.0.0.1:9386/agentbots/AGENT_ID/chat/completions \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Session-Id: sess-1" \
  -H 'Content-Type: application/json' \
  -d '{"query":"你好","stream":true,"customer":{"name":"张三"},"dialog":[{"role":"user","content":"之前的问题"},{"role":"assistant","content":"之前的回答"}]}'
```

Begin 组件若声明了输入键 `customer.name`，上面的 `customer.name` 会映射成该字段的 value。
