# QNZS Agent Bot（Go）

这个进程只提供一个接口：

`POST /agentbots/{agent_id}/chat/completions`

同时监听 `POST /api/v1/agentbots/{agent_id}/chat/completions`，方便接到 RAGFlow 现有的 `/api/v1` 前缀后面。

它对应自定义 Python 里的 `qnzs_agent_bot_completions`。处理函数是 `api/db/services/qnzs_canvas_service.py` 的 `qnzs_completion`（参考代码里写成 `from api.db.services.qnzs_canvas_service import qnzs_completion as agent_completion`），不是 `canvas_service.completion`。Go 做鉴权和 SSE，画布执行转到 `POST /internal/qnzs/canvas/run`，由 `qnzs_completion` 完成。

## 为什么执行留在 Python

仓库里已经有一套 Go agent/canvas 运行时，但它和检索、LLM、工具、存储绑在整个 `ragflow_server` 上，而且默认构建要链 DeepDoc 的 CGO 库。这个接口要单独编译、单独跑。Go 负责 Bearer 鉴权、`user_canvas` 归属、session 后缀和对外的 SSE 帧。内部接口把调用方的 JSON 原样交给 `qnzs_completion`（再加上已经校验过的 `tenant_id`、`agent_id` 和拼好的 `session_id`）。Begin 入参由 Python 的 `map_begin_inputs` 从 kwargs 组出来，Go 不会另塞一份 `inputs`。

`qnzs_completion` 和库存的 `canvas_service.completion` 不一样：

- 每次按 `agent_id` 读画布，不恢复 `API4Conversation`。
- `Canvas(dsl, tenant_id, agent_id)`：`agent_id` 是 task id，`canvas_id` 留空。
- `canvas.run` 不传 `session_id`。
- 请求里有 `dialog` 时，用它整表替换 `canvas.history`。
- 函数自己产出 `chat.completion.chunk` SSE。Go 取出 `delta.content` 后再按同样的帧格式写给客户端，所以 chunk id 和 Python 里生成的不同，正文一致。
- 参考代码里复用画布的分支是 `if False`，没有实现。每次请求新建 Canvas，生成器结束后调用 `close()` 并关掉它的线程池。

内部接口用 `QNZS_INTERNAL_TOKEN` 保护，没有这个环境变量时 Python 返回 503。

这个 fork 里没有单独的 QNZS 画布表。`QNZSUserCanvasService` 读现有的 `user_canvas`，并用 `user_id == tenant_id` 做归属校验。Go 在转发之前做同样的校验。

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
- Begin 入参由 `qnzs_completion` 读取 `components.begin` 的 input elements。键名里的 `a.b` 和 `a__b` 只取前两段，到正文的嵌套对象里取值；键名就是 `dialog` 时值为空字符串。正文里的 `dialog` 数组会整表替换画布 history。Go 里有一套相同规则的单测，线上请求不使用那份结果。
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
