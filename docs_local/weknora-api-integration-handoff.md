# WeKnora 外部平台问答接口交接文档

本文档给其他平台后端同事使用，说明如何把用户问题发送到当前 WeKnora 知识库，并把回答返回给业务平台。

## 1. 当前部署参数

| 项目 | 值 |
|---|---|
| Web 页面 | `http://服务器IP:9750/` |
| API Base URL | `http://服务器IP:9751/api/v1` |
| 健康检查 | `http://服务器IP:9751/health` |
| 知识库名称 | `test` |
| 知识库 ID | `c55df2b7-8fa8-44a4-adbe-4f18f372f7c4` |
| 默认 Agent | `builtin-quick-answer` |
| 调用方式 | 服务端到服务端 HTTP 调用 |
| 认证方式 | 请求头 `X-API-Key: <WEKNORA_API_KEY>` |

`<WEKNORA_API_KEY>` 由 WeKnora 管理员单独提供，不要写进前端页面、小程序、App 或公开仓库。

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

最少需要两个接口：

| 步骤 | 接口 | 说明 |
|---|---|---|
| 1 | `POST /sessions` | 创建对话会话，返回 `session_id` |
| 2 | `POST /knowledge-chat/{session_id}` | 向指定知识库提问，返回 SSE 流式回答 |

## 3. 创建会话

请求：

```bash
curl -sS -X POST 'http://服务器IP:9751/api/v1/sessions' \
  -H 'X-API-Key: <WEKNORA_API_KEY>' \
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
curl -N -X POST 'http://服务器IP:9751/api/v1/knowledge-chat/<SESSION_ID>' \
  -H 'X-API-Key: <WEKNORA_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "这里替换成用户问题",
    "knowledge_base_ids": ["c55df2b7-8fa8-44a4-adbe-4f18f372f7c4"],
    "agent_id": "builtin-quick-answer",
    "channel": "api",
    "disable_title": true
  }'
```

参数说明：

| 字段 | 必填 | 说明 |
|---|---|---|
| `query` | 是 | 用户问题 |
| `knowledge_base_ids` | 是 | 固定传 `test` 知识库 ID |
| `agent_id` | 建议传 | `builtin-quick-answer`，普通 RAG 问答 |
| `channel` | 建议传 | 固定传 `api`，方便日志区分调用来源 |
| `disable_title` | 建议传 | 固定传 `true`，避免每次调用额外生成标题 |

## 5. SSE 响应处理

`knowledge-chat` 返回的是 SSE，响应头类似：

```http
Content-Type: text/event-stream
```

响应片段示例：

```text
event: message
data: {"response_type":"references","content":"","done":false,"knowledge_references":[{"knowledge_title":"xxx.pdf","score":0.83}]}

event: message
data: {"response_type":"answer","content":"第一段回答","done":false}

event: message
data: {"response_type":"answer","content":"第二段回答","done":false}

event: message
data: {"response_type":"answer","content":"","done":true}
```

调用方处理规则：

| `response_type` | 处理方式 |
|---|---|
| `references` | 可选保存引用来源，用于展示“答案来自哪些文档” |
| `answer` + `done=false` | 按收到顺序拼接 `content` |
| `answer` + `done=true` | 本轮回答结束 |
| `error` | 按失败处理，记录错误内容 |

最终返回给业务平台的回答通常是：

```text
完整回答 = 所有 response_type=answer 且 done=false 的 content 按顺序拼接
```

## 6. 后端伪代码

```python
import json
import requests

BASE_URL = "http://服务器IP:9751/api/v1"
API_KEY = "<WEKNORA_API_KEY>"
KB_ID = "c55df2b7-8fa8-44a4-adbe-4f18f372f7c4"

headers = {
    "X-API-Key": API_KEY,
    "Content-Type": "application/json",
}

def create_session():
    resp = requests.post(
        f"{BASE_URL}/sessions",
        headers=headers,
        json={
            "title": "外部平台用户问答",
            "description": "由外部平台创建",
        },
        timeout=30,
    )
    resp.raise_for_status()
    body = resp.json()
    return body["data"]["id"]

def ask_weknora(session_id, question):
    resp = requests.post(
        f"{BASE_URL}/knowledge-chat/{session_id}",
        headers=headers,
        json={
            "query": question,
            "knowledge_base_ids": [KB_ID],
            "agent_id": "builtin-quick-answer",
            "channel": "api",
            "disable_title": True,
        },
        stream=True,
        timeout=180,
    )
    resp.raise_for_status()

    answer_parts = []
    references = []

    for line in resp.iter_lines(decode_unicode=True):
        if not line or not line.startswith("data:"):
            continue

        payload = json.loads(line[len("data:"):].strip())
        response_type = payload.get("response_type")

        if response_type == "references":
            references = payload.get("knowledge_references") or []

        elif response_type == "answer":
            if payload.get("done"):
                break
            answer_parts.append(payload.get("content") or "")

        elif response_type == "error":
            raise RuntimeError(payload.get("content") or "WeKnora answer failed")

    return {
        "answer": "".join(answer_parts),
        "references": references,
    }
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
curl -f 'http://服务器IP:9751/health'
```

服务端排查命令：

```bash
docker compose --profile full ps
docker compose logs --tail=200 app
```

常见问题：

| 现象 | 优先检查 |
|---|---|
| `401 Unauthorized` | `X-API-Key` 是否缺失、写错或已失效 |
| `404 Session not found` | `session_id` 是否来自当前 WeKnora，是否保存错 |
| 返回内容和 test 知识库无关 | `knowledge_base_ids` 是否固定为 `c55df2b7-8fa8-44a4-adbe-4f18f372f7c4` |
| SSE 一直没有结束 | 调用方超时时间是否太短；代理是否缓冲或截断流式响应 |
| 没有引用来源 | 问题可能未命中文档；可先在前端 test 知识库中验证同一问题 |
| 连接不上 API | 检查服务器防火墙、安全组、`WeKnora-app` 容器状态和 `9751` 端口 |

## 9. 给其他平台的约定

- API Key 只能放在其他平台后端服务，不允许下发到浏览器、小程序或 App。
- 请求超时建议设置为 `180s` 起步，长文档问答或大模型响应慢时可能需要更长。
- 每次请求建议记录业务请求 ID、WeKnora `session_id`、用户问题、最终回答和错误信息，方便排查。
- 生产环境如需区分不同终端用户的会话隔离，优先由业务平台保存 `业务用户/业务会话 -> session_id` 映射。
- 如果后续要开放公网或跨网络调用，建议在 `9751` 前加 HTTPS 反向代理，并限制来源 IP。
