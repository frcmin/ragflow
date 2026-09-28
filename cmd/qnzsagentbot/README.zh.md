# QNZS Agent Bot

这个接口挂在现有的 Go API 进程上，不另起一个画布运行时：

`POST /agentbots/{agent_id}/chat/completions`

同时监听 `POST /api/v1/agentbots/{agent_id}/chat/completions`。

它对应自定义 Python 里的 `qnzs_agent_bot_completions`，执行的是 `qnzs_completion` 的行为，画布本身用仓库里已经有的 Go canvas（`AgentService.buildRunFunc` + `canvas.Runner`），也就是 `/api/v1/agentbots/{agent_id}/completions` 同一条执行链。不调用 `canvas_service.completion`，也不再把请求转给 Python。

## 和库存 agentbot completion 的差别

库存接口会按 `session_id` 恢复 `API4Conversation`，task id 跟 session 走。这条 `chat/completions` 按 `qnzs_completion` 来：

- 每次从 `user_canvas` 的 DSL 起跑，先做 `canvas.reset()`（`dsl.ResetForCanvas`）。
- `agent_id` 是 task id，`canvas.run` 不带 session id，也不写 `API4Conversation`。
- 请求里有 `dialog` 时，用它整表替换 history。
- Begin 入参由 Go 的 `MapBeginInputs` 组好，再交给已有的 Begin 组件。键名里的 `a.b` / `a__b` 按 `split` 后的前两段取值；键名就是 `dialog` 时值为空字符串。
- 对外仍是 OpenAI `chat.completion.chunk`，`model` 为 `agent-bot`。画布内部的 `workflow_*` / `node_*` 帧不往外送，只取 `message` 的 content。编译或运行失败时，canvas runner 给出的 error 文本会写进同一条流。

归属校验是 `user_canvas.user_id == tenant_id`。这个 fork 没有单独的 QNZS 画布表。

## 请求行为

- `Authorization` 必须是两段（`Bearer <secret>`）。先按 `api_token.beta` 查，没有再按 `api_token.token` 查。失败时 HTTP 200，正文 `code` 为 102，文案与参考实现相同（含末尾引号）。`tenant_id` 来自命中的那一行。这条路由不走库存的 BetaAuthMiddleware（那边是 JWT，然后 token，再然后 beta）。
- `session_id` 取正文，否则取 `Session-Id` 头，再否则取正文里的 `Session-Id`。然后拼接成 `{session_id}-{agent_id}`。缺省时返回 `session_id is required.`，参考实现会在拼接时抛错。这个后缀不会传进 `canvas.run`。
- `stream` 缺省为 true。流式响应头：`Cache-Control: no-cache`、`Connection: keep-alive`、`X-Accel-Buffering: no`、`Content-Type: text/event-stream; charset=utf-8`。
- `stream` 为 false 时返回一条聚合后的 `chat.completion`（包在 `{"code":0,"data":...}` 里）。参考实现只返回第一帧。

## 调用示例

和 Go API 同一个进程、同一个端口（默认 9380）：

```bash
curl -N http://127.0.0.1:9380/agentbots/AGENT_ID/chat/completions \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Session-Id: sess-1" \
  -H 'Content-Type: application/json' \
  -d '{"query":"你好","stream":true,"customer":{"name":"张三"},"dialog":[{"role":"user","content":"之前的问题"},{"role":"assistant","content":"之前的回答"}]}'
```

Begin 组件若声明了输入键 `customer.name`，上面的嵌套字段会映射成该输入的 value。
