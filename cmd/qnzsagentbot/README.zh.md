# QNZS Agent Bot（纯 Go）

这个进程只提供一个接口：

`POST /agentbots/{agent_id}/chat/completions`

同时监听 `POST /api/v1/agentbots/{agent_id}/chat/completions`。

没有文档解析、入库或数据集接口。画布执行直接调用仓库里已有的 Go canvas：`AgentService.buildRunFunc` 和 `canvas.Runner`。不把请求转给 Python，也不调用 `canvas_service.completion`。

请求处理是一层薄壳：Bearer 鉴权、session 后缀、按 id 读取 `user_canvas`、用 Begin 的 inputs 组参数、调用 Go canvas、再把 `message` 内容写成 OpenAI SSE。

## 和库存 agentbot completion 的差别

库存 `/api/v1/agentbots/{id}/completions` 会按 session 恢复 `API4Conversation`。这条 `chat/completions` 按 `qnzs_completion` 来：

- 每次从 `user_canvas` 的 DSL 起跑，先做与 `canvas.reset()` 对应的 `dsl.ResetForCanvas`。
- `agent_id` 是 task id。`canvas.run` 不带 session id，也不写 `API4Conversation`。
- 请求里有 `dialog` 时，用它整表替换 history。
- Begin 入参由 `MapBeginInputs` 组好后交给已有的 Begin 组件。`a.b` 和 `a__b` 用 `split` 后的前两段取值。键名是 `dialog` 时值为空字符串。
- 对外是 `chat.completion.chunk`，`model` 为 `agent-bot`。`workflow_*` 和 `node_*` 不往外送。canvas runner 的 error 文本会写进同一条流。

这个 fork 没有单独的 QNZS 画布表。归属是 `user_canvas.user_id == tenant_id`。

## 构建和启动

需要 Go 1.27。这个二进制不链 DeepDoc，`CGO_ENABLED=0`。

数据库和模型配置用 RAGFlow 的 `conf/service_conf.yaml`（或 `RAGFLOW_CONFIG`）。MySQL 必须能读到 `api_token`、`user_canvas` 和租户模型。进程用 `dao.InitDB` 打开这条库，但不跑完整 `--migrate`。启动仍会补齐 Go 运行时表（含会话拆表和 ingestion 任务表）并加载 `conf/models`，因为画布里的 LLM 节点走同一套凭证解析。HTTP 上仍然只有这一条路由。

```bash
bash cmd/qnzsagentbot/build.sh
export RAGFLOW_CONFIG=/path/to/service_conf.yaml
export QNZS_LISTEN=:9386
./bin/qnzsagentbot
```

## 请求行为

- `Authorization` 必须是两段（`Bearer <secret>`）。先按 `api_token.beta` 查，没有再按 `api_token.token` 查。失败时 HTTP 200，正文 `code` 为 102，文案与参考实现相同（含末尾引号）。
- `session_id` 取正文，否则取 `Session-Id` 头，再否则取正文里的 `Session-Id`，然后拼成 `{session_id}-{agent_id}`。缺省时返回 `session_id is required.`。这个后缀不传进 canvas run。
- `stream` 缺省为 true。响应头：`Cache-Control: no-cache`、`Connection: keep-alive`、`X-Accel-Buffering: no`、`Content-Type: text/event-stream; charset=utf-8`。
- `stream` 为 false 时返回聚合成一条的 `chat.completion`（包在 `{"code":0,"data":...}` 里）。参考实现只返回第一帧。

## 调用示例

```bash
curl -N http://127.0.0.1:9386/agentbots/AGENT_ID/chat/completions \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Session-Id: sess-1" \
  -H 'Content-Type: application/json' \
  -d '{"query":"你好","stream":true,"customer":{"name":"张三"},"dialog":[{"role":"user","content":"之前的问题"},{"role":"assistant","content":"之前的回答"}]}'
```

Begin 若声明了 `customer.name`，上面的嵌套字段会成为该输入的 value。
