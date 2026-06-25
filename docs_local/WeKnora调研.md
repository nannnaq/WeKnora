# WeKnora 项目详细介绍

## 1. 项目定位

- 功能完备，可直接使用。

- 多模态支持：文档、表格、图片、语音等。
- RAG 问答：基于关键词、向量、重排和上下文生成回答，可返回问题相关的图片和表格。
- 多Agent ：可在快速问答、智能推理、维基问答、数据分析师四个问答智能体中切换。
- Wiki 和知识图谱：由 Agent 从原始文档生成结构化 Markdown Wiki 页面和知识图谱。
- 多租户协作：租户、共享空间、RBAC、审计日志、API Key、系统管理员能力。
- 第三方支持：可接入钉钉、飞书、微信、Telegram等。

## 2. 文本问答测试

**问：SCX-20/21的四天线设计如何提升航向精度和可靠性？**

**答：**（优点：可以基于文档解析后得到的Wiki 页面之间的引用关系组织回答，并且返回相关图片。）

![image-20260625175106731](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625175106731.png)

![image-20260625175124560](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625175124560.png)

---

## 3. 图片问答测试

**问：**

![image-20260625175703160](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625175703160.png)

**答：**（实现逻辑：先调用视觉模型理解图片并将图片转为文本描述，之后召回相关文档片段组织回答。）

![image-20260625175725416](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625175725416.png)

![image-20260625175738686](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625175738686.png)

**原始文档中该问题对应的片段：**

![image-20260625180223666](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625180223666.png)

---

## 4. 软件功能页面展示

### 4.1 文档上传与解析

![image-20260625180405083](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625180405083.png)

![image-20260625180556822](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625180556822.png)

![image-20260625180849022](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625180849022.png)

### 4.2 文档转为wiki

![image-20260625181113841](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625181113841.png)

![image-20260625181503016](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625181503016.png)

![image-20260625182019045](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182019045.png)

### 4.3 文档转为wiki图谱

注：前端页面展示的图谱是**wiki页面之间的引用关系图**，并不是图数据库中实际的实体和关系。Weknora支持图数据库，在文档分片时会抽取实体和关系存入图数据库，对话时系统会自动根据意图查询图谱并返回补充信息（这部分的逻辑和LightRAG类似）。

![image-20260625182427851](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182427851.png)

![image-20260625182525194](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182525194.png)

![image-20260625182704172](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182704172.png)

### 4.4 问答页面

![image-20260625182828147](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182828147.png)

### 4.5 其他通用设置

![image-20260625182928989](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625182928989.png)

![image-20260625183007395](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625183007395.png)

![image-20260625183029261](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625183029261.png)

![image-20260625183135857](C:\Users\CASIA\AppData\Roaming\Typora\typora-user-images\image-20260625183135857.png)

### 4.1 主要服务

| 服务 | 代码或配置位置 | 职责 |
| --- | --- | --- |
| frontend | `frontend/`、`frontend/Dockerfile` | Vue 3 Web UI，容器中通过 Nginx 对外提供页面并代理 API。 |
| app | `cmd/server`、`internal/`、`docker/Dockerfile.app` | Go/Gin 后端，提供 REST API、认证、租户、知识库、RAG、Agent、任务调度、模型调用、IM 回调等能力。 |
| docreader | `docreader/`、`docker/Dockerfile.docreader` | gRPC 文档解析服务，负责文件读取、结构抽取、PDF 渲染和解析结果返回。 |
| postgres | `docker-compose.yml` | ParadeDB/PostgreSQL，存储业务数据、迁移表、pgvector embedding、BM25/全文检索相关能力。 |
| redis | `docker-compose.yml` | 流式状态、异步任务队列、缓存、Langfuse 复用队列等。 |
| minio | `docker-compose.yml` profile | 可选对象存储。默认 `STORAGE_TYPE=local` 时不是主存储，需显式切换到 `minio`。 |
| neo4j | `docker-compose.yml` profile | 可选知识图谱存储。需要 `NEO4J_ENABLE=true` 并在知识库配置中启用图谱抽取。 |
| qdrant | `docker-compose.yml` profile | 可选向量库。全量 profile 会启动，但默认 `RETRIEVE_DRIVER=postgres`。 |
| langfuse | `docker-compose.yml` profile | 可选可观测性栈，包含 Langfuse Web、Worker、ClickHouse、专用 MinIO。 |
| searxng | `docker-compose.yml` profile | 可选自建网页搜索服务。配置搜索提供者时容器内地址为 `http://searxng:8080`。 |
| mcp | `mcp-server/` | WeKnora MCP Server，把 WeKnora API 暴露为 MCP 工具。需要 `WEKNORA_API_KEY`。 |
| sandbox | `docker/Dockerfile.sandbox` | Agent Skills 执行镜像准备项，不是常驻业务服务。 |

## 5. 技术栈

### 5.1 后端

- 语言与框架：Go `1.26.0`，Gin，GORM，dig 依赖注入。
- 数据库：PostgreSQL/ParadeDB，SQLite 用于 Lite；业务迁移基于 `golang-migrate`。
- 异步任务：Asynq + Redis，处理文档解析、向量化、摘要、问题生成、Wiki 入库等任务。
- 检索：PostgreSQL/pgvector、BM25、父子分块、多向量库扇出、Rerank。
- 模型接入：OpenAI 兼容接口以及多个厂商 SDK/适配器。
- 工具系统：内置工具、MCP 工具、网页搜索、Agent Skills 沙箱。
- 可观测：Langfuse tracing，文档解析 span 时间线。

### 5.2 前端

- Vue 3、Vite、TypeScript、Pinia、Vue Router。
- TDesign Vue Next 作为主要组件库。
- 支持 Markdown、KaTeX、Mermaid、Office/PPT/Word 预览、虚拟列表等。
- 页面模块包括登录注册、知识库、聊天、Agent、系统设置、平台设置、共享空间、Wiki、集成配置等。

### 5.3 文档解析

DocReader 是独立 gRPC 服务，负责将上传文件转成后端可处理的结构化内容。其职责包括：

- 读取 PDF、Word、PPT、Excel、CSV、Markdown、HTML、图片等格式。
- PDF 页面渲染、图片输出、解析并发控制。
- 将扫描页图片交由 Go App 侧调用 VLM/OCR 模型处理。
- 支持 gRPC TLS 与 Token 认证配置。
- 可选 OpenDataLoader hybrid 后端，但该镜像不随默认 `full` profile 启动。

## 6. 关键业务流程

### 6.2 文档入库

```text
上传文件 / URL / 手工内容 / 数据源同步
        |
        v
创建 Knowledge 记录，提交异步任务
        |
        v
DocReader 解析文件，抽取文本、图片、表格等
        |
        v
按知识库策略分块：auto / heading / heuristic / legacy
        |
        v
调用 Embedding 模型生成向量
        |
        v
写入默认或绑定的向量库
        |
        v
可选后处理：摘要、问题生成、图谱抽取、Wiki 入库
```

入库是否成功，取决于模型配置是否完整。至少需要可用的 Embedding 模型；如果要问答，还需要可用的 KnowledgeQA/LLM 模型。图片、扫描件、音频等场景还可能需要 VLM 或 ASR。

### 6.3 RAG 快速问答

1. 用户在 Web UI、REST API、CLI 或 IM 发起问题。
2. 后端读取会话、租户、知识库和模型配置。
3. 可选执行 query rewrite / query expansion。
4. 进入检索流程：关键词、向量、图谱增强、父子分块等策略按配置执行。
5. 可选使用 Rerank 模型重排候选块。
6. 将上下文、Prompt、历史会话传给 LLM。
7. 流式返回答案，并记录消息、引用来源和可观测 trace。

### 6.4 Agent 推理

Agent 模式面向更复杂任务。它不是一次检索后直接回答，而是通过 ReAct 类流程多轮思考和调用工具：

- 调用知识库检索工具获取上下文。
- 调用 MCP 工具访问外部系统。
- 调用网页搜索提供者补充公开信息。
- 调用 Agent Skills 执行预定义脚本或流程。
- 根据工具结果继续推理，直到生成最终回答。

对需要人工确认的 MCP 工具，WeKnora 支持人机审批；等待超时可通过 `WEKNORA_AGENT_TOOL_APPROVAL_TIMEOUT` 配置。

### 6.5 Wiki 模式

Wiki 模式用于把原始文档转成结构化知识页面：

1. 在知识库索引策略中启用 Wiki。
2. 上传文档或触发重新入库。
3. 后端异步分析文档核心概念、页面结构、链接关系和来源引用。
4. 生成或更新 Markdown Wiki 页面。
5. 前端提供 Wiki 浏览器和可视化图谱。

如果同时启用 Neo4j 和 GraphRAG，系统还可以把实体关系写入图数据库，用于检索增强和图谱展示。

## 7. 功能模块

### 7.1 知识管理

- 知识库类型：文档、FAQ、Wiki。
- 导入方式：文件上传、文件夹导入、URL 导入、手工录入、外部数据源同步。
- 格式覆盖：PDF、Word、Text、Markdown、HTML、图片、CSV、Excel、PPT、JSON 等。
- 分块能力：自适应三层分块、父子分块、分块预览、按批次 `process_config` 覆盖。
- 文档追踪：解析阶段 span 时间线、处理中断、失败排查。
- 批量操作：知识库文档列表支持框选多选等批量管理能力。

### 7.2 检索与问答

- BM25 稀疏召回、Dense 向量召回、Rerank 重排。
- PostgreSQL/pgvector 默认向量检索，支持 HNSW 索引。
- 可绑定外部或内置向量库。
- 可配置上下文窗口、召回数、阈值、重写、扩展、兜底策略。
- 支持推荐问题、摘要生成、会话标题生成。

### 7.3 Agent 与工具

- 自定义 Agent：可配置提示词、模型、关联知识库、运行范围。
- 内置工具：知识检索、数据分析、网页搜索等。
- MCP：支持 SSE、HTTP Streamable、Stdio 传输方式。
- Agent Skills：支持预装技能和自定义技能目录。
- 工具审批：对敏感 MCP 工具进行人工确认。

### 7.4 数据源与外部集成

仓库当前包含飞书、Notion、语雀、RSS 等 connector 代码，并提供数据源同步文档。IM 层包含企业微信、飞书、Slack、Telegram、钉钉、Mattermost、微信等适配器。

### 7.5 平台与安全

- 多租户工作区与自助创建。
- 租户 RBAC：Viewer、Contributor、Admin、Owner。
- 跨租户共享空间。
- API Key 与 JWT 认证。
- 系统管理员与平台设置。
- 租户审计日志。
- API Key、MCP 凭据、数据源凭据等敏感字段加密。
- SSRF 防护与白名单。
- DocReader gRPC TLS/Token 可选加固。

