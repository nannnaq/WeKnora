# WeKnora 外部平台问答接口交接文档

本文档给其他平台后端同事使用，说明如何把用户问题发送到当前 WeKnora 知识库，并把回答返回给业务平台。

> **当前联调状态（2026-08-18）**：本文档中的地址、API Key、Tenant ID、知识库 ID、
> Agent、创建会话和知识库问答均已在 `192.168.18.103` 完成真实联调。问答请求可以
> 返回引用、非空回答和最终 `complete` 事件。当前聊天/多模态模型为
> `qwen3-8-27b`，服务地址为 `http://192.168.18.152:7860/v1`，默认关闭思考模式。

## 1. 当前部署参数

| 项目 | 值 |
|---|---|
| Web 页面 | `http://192.168.18.103:9750/` |
| API Base URL | `http://192.168.18.103:9751/api/v1` |
| 健康检查 | `http://192.168.18.103:9751/health` |
| X-API-Key | `sk-wp6K_te2VMw5pb3EAGnduVSQoSDKS4lr_rgf10zXESZRfhYz` |
| X-Tenant-ID | `10000` |
| 知识库名称 | `test` |
| 知识库 ID | `c55df2b7-8fa8-44a4-adbe-4f18f372f7c4` |
| 默认 Agent | `builtin-quick-answer` |
| 聊天/多模态模型 | `qwen3-8-27b` |
| 模型服务地址 | `http://192.168.18.152:7860/v1` |
| 思考模式 | 默认关闭，模型请求传 `chat_template_kwargs.enable_thinking=false` |
| 调用方式 | 服务端到服务端 HTTP 调用 |
| 认证方式 | 必须同时传 `X-API-Key` 和 `X-Tenant-ID: 10000` |

本文档已包含可直接调用的明文 API Key，不需要再向管理员索取其他凭据。该凭据只允许
保存在其他平台的后端服务中，不得下发给浏览器、小程序或 App。本文档按内部敏感资料
管理，只能在受控内网中传递，不得上传到公网、公开代码仓库或对外文档平台。

调用方可以直接复制以下固定配置：

```text
WEKNORA_BASE_URL=http://192.168.18.103:9751/api/v1
WEKNORA_API_KEY=sk-wp6K_te2VMw5pb3EAGnduVSQoSDKS4lr_rgf10zXESZRfhYz
WEKNORA_TENANT_ID=10000
WEKNORA_KNOWLEDGE_BASE_ID=c55df2b7-8fa8-44a4-adbe-4f18f372f7c4
WEKNORA_AGENT_ID=builtin-quick-answer
```

普通问答不需要传 `Authorization`。如调用方有链路追踪需求，可额外传
`X-Request-ID: <业务请求ID>`；不传时 WeKnora 会自动生成。

## 2. 推荐调用流程

推荐按“业务用户/业务会话”保留 WeKnora 的 `session_id`，这样可以支持多轮上下文。

```text
其他平台后端
  1. 没有 session_id 时，调用 WeKnora 创建会话
  2. 保存返回的 session_id
  3. 用户每次提问时，调用 knowledge-chat
  4. 读取 SSE 流，拼接 answer 内容
  5. 把完整回答返回给其他平台
```

最少需要两个接口，均使用 HTTP `POST`：

| 步骤 | 完整接口 | 成功响应 | 说明 |
|---|---|---|---|
| 1 | `POST http://192.168.18.103:9751/api/v1/sessions` | HTTP `201`、JSON | 创建对话会话，返回 `session_id` |
| 2 | `POST http://192.168.18.103:9751/api/v1/knowledge-chat/{session_id}` | HTTP `200`、SSE | 向固定知识库提问，返回流式回答 |

## 3. 创建会话

请求：

```bash
curl -sS -X POST 'http://192.168.18.103:9751/api/v1/sessions' \
  -H 'X-API-Key: sk-wp6K_te2VMw5pb3EAGnduVSQoSDKS4lr_rgf10zXESZRfhYz' \
  -H 'X-Tenant-ID: 10000' \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "外部平台用户问答",
    "description": "由外部平台创建"
  }'
```

成功响应示例：

```json
{
  "success": true,
  "data": {
    "id": "11111111-2222-3333-4444-555555555555",
    "title": "外部平台用户问答",
    "tenant_id": 10000
  }
}
```

调用方需要保存：

```text
session_id = data.id
```

建议保存关系：

```text
业务平台用户 ID + 业务会话 ID -> WeKnora session_id
```

## 4. 向知识库提问

请求：

```bash
curl -sS -N -X POST 'http://192.168.18.103:9751/api/v1/knowledge-chat/<SESSION_ID>' \
  -H 'X-API-Key: sk-wp6K_te2VMw5pb3EAGnduVSQoSDKS4lr_rgf10zXESZRfhYz' \
  -H 'X-Tenant-ID: 10000' \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{
    "query": "这里替换成用户问题",
    "knowledge_base_ids": ["c55df2b7-8fa8-44a4-adbe-4f18f372f7c4"],
    "agent_enabled": false,
    "agent_id": "builtin-quick-answer",
    "web_search_enabled": false,
    "channel": "api",
    "disable_title": true
  }'
```

参数说明：

| 字段 | 必填 | 说明 |
|---|---|---|
| `query` | 是 | 用户问题 |
| `knowledge_base_ids` | 是 | 固定传 `test` 知识库 ID |
| `agent_enabled` | 是 | 固定传 `false`，使用快速回答模式 |
| `agent_id` | 是 | 固定传 `builtin-quick-answer`，普通 RAG 问答 |
| `web_search_enabled` | 是 | 固定传 `false`，只使用指定知识库，不访问互联网 |
| `channel` | 是 | 固定传 `api`，方便日志区分调用来源 |
| `disable_title` | 是 | 固定传 `true`，避免每次调用额外生成标题 |

`<SESSION_ID>` 必须替换成创建会话接口返回的 `data.id`，不能传业务平台自己的
会话 ID。请求成功后连接会保持打开，调用方需要持续读取响应流，直到收到
`response_type=complete`。

## 5. SSE 响应处理

`knowledge-chat` 返回的是 SSE，响应头类似：

```http
Content-Type: text/event-stream
```

响应片段示例：

```text
event: message
data: {"id":"req-xxx","response_type":"references","content":"","done":false,"knowledge_references":[{"knowledge_title":"xxx.pdf","score":0.83}]}

event: message
data: {"id":"req-xxx","response_type":"answer","content":"第一段回答","done":false}

event: message
data: {"id":"req-xxx","response_type":"answer","content":"第二段回答","done":false}

event: message
data: {"id":"req-xxx","response_type":"answer","content":"","done":true}

event: message
data: {"id":"req-xxx","response_type":"complete","content":"","done":true,"data":{"total_steps":1,"total_duration_ms":1234}}
```

调用方处理规则：

| `response_type` | 处理方式 |
|---|---|
| `references` | 可选保存引用来源，用于展示“答案来自哪些文档” |
| `answer` | 不论 `done` 值，均按收到顺序拼接非空 `content`；`done=true` 只表示该回答事件结束 |
| `complete` + `done=true` | 本轮请求正常结束，停止读取 SSE |
| `error` | 按失败处理，记录 `content`；`done=true` 时停止读取 SSE |
| `stop` | 回答被服务端或用户停止，按未完整回答处理 |

最终返回给业务平台的回答通常是：

```text
完整回答 = 所有 response_type=answer 的非空 content 按顺序拼接
```

每个 SSE 事件由 `event:` 行、一个或多个 `data:` 行和空行分隔。生产代码应按空行
组装完整事件后再解析 JSON，不要假定一次网络读取正好对应一条事件。收到 HTTP 非
2xx 响应时，响应体通常是普通 JSON 错误而不是 SSE，应先检查 HTTP 状态码再进入流式解析。

## 6. 后端伪代码

```python
import json
import uuid
import requests

BASE_URL = "http://192.168.18.103:9751/api/v1"
API_KEY = "sk-wp6K_te2VMw5pb3EAGnduVSQoSDKS4lr_rgf10zXESZRfhYz"
TENANT_ID = "10000"
KB_ID = "c55df2b7-8fa8-44a4-adbe-4f18f372f7c4"

def request_headers(accept="application/json"):
    return {
        "X-API-Key": API_KEY,
        "X-Tenant-ID": TENANT_ID,
        "X-Request-ID": str(uuid.uuid4()),
        "Content-Type": "application/json",
        "Accept": accept,
    }

def create_session():
    resp = requests.post(
        f"{BASE_URL}/sessions",
        headers=request_headers(),
        json={
            "title": "外部平台用户问答",
            "description": "由外部平台创建",
        },
        timeout=30,
    )
    resp.raise_for_status()
    body = resp.json()
    return body["data"]["id"]

def iter_sse_data(resp):
    """按 SSE 空行边界组装事件，并返回 data 字段。"""
    data_lines = []
    for line in resp.iter_lines(decode_unicode=True):
        if line == "":
            if data_lines:
                yield "\n".join(data_lines)
                data_lines = []
            continue
        if line.startswith("data:"):
            data_lines.append(line[len("data:"):].lstrip())
    if data_lines:
        yield "\n".join(data_lines)

def ask_weknora(session_id, question):
    answer_parts = []
    references = []
    completed = False

    with requests.post(
        f"{BASE_URL}/knowledge-chat/{session_id}",
        headers=request_headers("text/event-stream"),
        json={
            "query": question,
            "knowledge_base_ids": [KB_ID],
            "agent_enabled": False,
            "agent_id": "builtin-quick-answer",
            "web_search_enabled": False,
            "channel": "api",
            "disable_title": True,
        },
        stream=True,
        timeout=(10, 180),
    ) as resp:
        resp.raise_for_status()

        for data in iter_sse_data(resp):
            payload = json.loads(data)
            response_type = payload.get("response_type")

            if response_type == "references":
                references = payload.get("knowledge_references") or []

            elif response_type == "answer":
                answer_parts.append(payload.get("content") or "")

            elif response_type == "complete" and payload.get("done"):
                completed = True
                break

            elif response_type == "error":
                raise RuntimeError(payload.get("content") or "WeKnora answer failed")

            elif response_type == "stop":
                raise RuntimeError("WeKnora answer was stopped before completion")

    if not completed:
        raise RuntimeError("WeKnora SSE stream ended without a complete event")

    return {
        "answer": "".join(answer_parts),
        "references": references,
    }

session_id = create_session()
result = ask_weknora(session_id, "这里替换成用户问题")
print(result["answer"])
```

## 7. 多轮对话建议

同一个用户在同一个业务会话中，复用同一个 WeKnora `session_id`：

```text
用户第一次提问：
  创建 session -> 保存 session_id -> 调用 knowledge-chat

用户继续追问：
  读取已保存 session_id -> 直接调用 knowledge-chat
```

如果业务平台不需要上下文，也可以每次提问都新建 session；但这样用户追问“它是什么意思”“继续说明”这类问题时，知识库不会自动继承上一轮上下文。

## 8. 健康检查和排查

健康检查：

```bash
curl -f 'http://192.168.18.103:9751/health'
```

服务端排查命令：

```bash
cd /mnt/storage/opt/product/services/weknora
./scripts/status.sh
docker compose logs --tail=200 app
```

常见问题：

| 现象 | 优先检查 |
|---|---|
| `401 Unauthorized` | `X-API-Key` 缺失、写错或已失效；确认没有额外空格或被代理删除 |
| `409 TENANT_REQUIRED` | API Key 已识别，但缺少 `X-Tenant-ID`；补传 `X-Tenant-ID: 10000` |
| `403 Forbidden` | Tenant ID 与 API Key 所属工作区不匹配，或 API Key 无目标知识库权限 |
| `404 Session not found` | `session_id` 是否来自当前 WeKnora，是否保存错 |
| 返回内容和 test 知识库无关 | `knowledge_base_ids` 是否固定为 `c55df2b7-8fa8-44a4-adbe-4f18f372f7c4` |
| SSE 一直没有结束 | 调用方超时时间是否太短；代理是否缓冲或截断流式响应 |
| 检索完成但迟迟没有 `answer` / `complete` | 检查 WeKnora 服务端是否能访问 `192.168.18.152:7860`，以及模型 `qwen3-8-27b` 是否在线 |
| 没有引用来源 | 问题可能未命中文档；可先在前端 test 知识库中验证同一问题 |
| 连接不上 API | 检查服务器防火墙、安全组、`WeKnora-app` 容器状态和 `9751` 端口 |

## 9. 给其他平台的约定

- API Key 和 Tenant ID 只能放在其他平台后端服务，不允许下发到浏览器、小程序或 App。
- 请求读取超时建议设置为 `180s` 起步，并设置业务总截止时间；超时或流结束前未收到 `complete` 时按失败处理。
- 每次请求建议记录业务请求 ID、WeKnora `session_id`、用户问题、最终回答和错误信息，方便排查。
- 生产环境如需区分不同终端用户的会话隔离，优先由业务平台保存 `业务用户/业务会话 -> session_id` 映射。
- 如果后续要开放公网或跨网络调用，建议在 `9751` 前加 HTTPS 反向代理，并限制来源 IP。
