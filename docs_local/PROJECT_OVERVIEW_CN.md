# WeKnora 项目详细介绍

## 1. 项目定位

WeKnora 是一个开源的 LLM 知识管理与 RAG 应用框架，面向企业级文档理解、语义检索、智能问答、Agent 推理和自动 Wiki 构建。它把原始文档、FAQ、网页、外部数据源等知识资产转成可检索、可追踪、可协作、可通过 API/CLI/IM 调用的知识服务。

项目的核心价值不是单纯“上传文档后问答”，而是提供一套完整知识平台能力：

- 文档入库：解析多种文件格式，切分、向量化、索引、后处理。
- RAG 问答：基于关键词、向量、重排和上下文生成回答。
- Agent 推理：使用 ReAct 流程调度知识检索、MCP 工具、网页搜索和自定义技能。
- Wiki 模式：由 Agent 从原始文档生成结构化 Markdown Wiki 页面和知识图谱。
- 多租户协作：租户、共享空间、RBAC、审计日志、API Key、系统管理员能力。
- 私有化部署：通过 Docker Compose、Helm、Lite 版本或源码运行部署。

## 2. 适用场景

WeKnora 适合以下类型的团队或系统：

- 企业内部知识库：制度、产品文档、研发文档、客服知识、销售材料、培训资料。
- 垂直领域问答：把 PDF、Word、Markdown、网页、表格等沉淀成可问答知识资产。
- Agent 助手：让 Agent 在回答复杂问题时调用企业知识库、外部工具和网页搜索。
- 自动化知识整理：通过 Wiki 模式把散乱文档转成页面、链接和图谱。
- 多租户 SaaS 或内部平台：按工作区隔离知识库、模型、成员权限和审计记录。
- IM 机器人：接入企业微信、飞书、Slack、Telegram、钉钉、Mattermost、微信等渠道。

不适合作为默认选择的场景：

- 只需要一个极简本地单文件知识库，且不需要账号、权限、任务队列和 Web UI。
- 不能提供任何 LLM/Embedding 模型服务的环境。容器能启动，但文档向量化和问答不可用。
- 资源极低的机器上直接启动所有可选组件。`full` profile 会额外启动 Langfuse、ClickHouse、MinIO、Neo4j、Qdrant 等服务。

## 3. 核心概念

| 概念 | 说明 |
| --- | --- |
| Tenant / 工作区 | 多租户隔离单元。用户可以创建或加入多个工作区，不同工作区拥有独立知识库、模型和成员权限。 |
| User / 用户 | 登录主体。用户通过 JWT 使用 Web UI，也可通过 API Key 访问 REST API。 |
| Tenant RBAC | 租户内角色控制，包含 `Owner`、`Admin`、`Contributor`、`Viewer`。写操作会结合角色和资源归属判断。 |
| Knowledge Base / 知识库 | 知识组织和检索配置单元。可绑定模型、向量库、分块策略、索引策略、Wiki/图谱配置。 |
| Knowledge / 知识 | 上传的文件、URL、手工内容、FAQ、外部数据源同步内容等。 |
| Chunk / 分块 | 文档解析后用于 embedding 和检索的小片段。支持自适应分块、父子分块、预览调试。 |
| Model / 模型 | LLM、Embedding、Rerank、VLM、ASR 等模型配置。可由租户配置，也可由系统管理员用 YAML 声明为内置模型。 |
| Vector Store / 向量库 | 存储 embedding 的后端。支持 PostgreSQL/pgvector、Qdrant、Milvus、Weaviate、OpenSearch、Elasticsearch、Doris、腾讯云 VectorDB 等。 |
| Session / 会话 | 用户与知识库或 Agent 的对话上下文。 |
| Agent | 自定义智能体，可关联知识库、工具、MCP 服务、网页搜索和 Skills。 |
| MCP Service | Model Context Protocol 工具服务，用于把外部工具安全接入 Agent。 |
| Wiki Page | Wiki 模式生成或维护的 Markdown 页面，可构成知识页面网络和可视化图谱。 |
| Data Source | 外部数据源同步配置，如飞书、Notion、语雀、RSS 等。 |
| IM Channel | 即时通讯渠道配置，用于在 IM 中直接问答。 |

## 4. 总体架构

WeKnora 标准版是多服务架构，默认 Docker Compose 栈由前端、后端、文档解析服务、数据库、Redis 和若干可选组件组成。

```text
Browser / CLI / API / IM / MCP Client
                 |
                 v
        Frontend Nginx / Web UI
                 |
                 v
        Go App API Server (/api/v1)
          |        |          |
          |        |          +--> LLM / Embedding / Rerank / VLM / ASR providers
          |        |
          |        +--> DocReader gRPC document parser
          |
          +--> PostgreSQL / ParadeDB / pgvector
          +--> Redis / Asynq task queue / stream manager
          +--> Object storage: local / MinIO / S3-compatible / cloud storage
          +--> Optional vector stores: Qdrant / Milvus / Weaviate / Doris / OpenSearch
          +--> Optional graph store: Neo4j
          +--> Optional observability: Langfuse
          +--> Optional web search: SearXNG and other providers
```

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

### 6.1 启动与初始化

1. `app` 加载 `config/config.yaml` 和环境变量。
2. 构建依赖注入容器，初始化数据库、Redis、DocReader、对象存储、模型服务、检索引擎、Langfuse 等组件。
3. 默认执行 `migrations/versioned` 数据库迁移；可用 `AUTO_MIGRATE=false` 关闭。
4. 读取可选 `config/builtin_models.yaml`，将声明式内置模型 upsert 到 `models` 表。
5. 如设置 `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL`，且该用户已注册且当前没有系统管理员，则启动时提升为系统管理员。
6. 注册 `/api/v1` 路由和 IM/embed 等公开回调路由。

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

## 8. API、CLI 与客户端

### 8.1 REST API

后端基础路径为 `/api/v1`。仓库在 `docs/api/` 中维护按模块拆分的接口文档；非 release 模式下还提供 Swagger UI：

```text
http://localhost:8080/swagger/index.html
```

认证方式包括 JWT Bearer Token 和 `X-API-Key`。API 模块覆盖认证、租户、知识库、知识、模型、会话、聊天、MCP 服务、向量库、网页搜索、数据源、IM 等。

### 8.2 CLI

`cli/` 下提供官方命令行工具 `weknora`，用于：

- 管理 profile 与登录凭证。
- 管理知识库和文档。
- 上传文档、等待解析、检索 chunks。
- 进行流式 RAG 问答。
- 管理 Agent 和会话。
- 以 MCP Server 方式暴露工具面。

### 8.3 MCP Server

`mcp-server/` 是 Python MCP Server，用于把 WeKnora REST API 映射为 MCP 工具。Docker Compose `full` profile 会启动 `mcp` 容器，但需要先在 WeKnora 前端生成 API Key，并通过 `.env` 的 `WEKNORA_API_KEY` 注入。

### 8.4 微信小程序与 Chrome 插件

仓库包含 `miniprogram/` 微信小程序代码；公开 README 还说明有 Chrome 插件，可把网页内容采集到 WeKnora 知识库。

## 9. 部署形态

| 形态 | 适用场景 |
| --- | --- |
| Docker Compose 标准版 | 本地、单机、内网私有化、快速验证完整能力。推荐主线部署方式。 |
| Docker Compose 开发模式 | 本地开发，基础设施在 Docker，后端和前端本地热更新。 |
| Helm Chart | Kubernetes 生产部署。仓库 `helm/` 提供 Chart 和 values。 |
| Lite 版本 | 单应用、SQLite、零外部依赖，适合个人本地轻量试用。功能少于标准版。 |
| 源码运行 | 开发或定制场景，需要自行准备数据库、Redis、DocReader 等依赖。 |

## 10. 目录导览

| 路径 | 说明 |
| --- | --- |
| `cmd/server/` | 标准版 Go 后端入口。 |
| `internal/` | 后端主要业务、路由、模型、检索、Agent、IM、MCP、数据源、配置、迁移等实现。 |
| `frontend/` | Vue 前端应用。 |
| `docreader/` | 文档解析服务说明与代码。 |
| `migrations/` | PostgreSQL、SQLite、ParadeDB 初始化与版本迁移脚本。 |
| `docker-compose.yml` | 标准版 Docker Compose 主编排文件。 |
| `docker-compose.dev.yml` | 开发模式基础设施编排文件。 |
| `docker/` | 后端、docreader、sandbox、ODL hybrid 等 Dockerfile。 |
| `config/` | 后端配置、Prompt 模板、内置模型 YAML 示例。 |
| `docs/` | 产品、功能、API、部署、排障与开发文档。 |
| `cli/` | 官方 CLI。 |
| `mcp-server/` | Python MCP Server。 |
| `skills/preloaded/` | 预装 Agent Skills。 |
| `helm/` | Kubernetes Helm Chart。 |
| `miniprogram/` | 微信小程序。 |

## 11. 运维关注点

- 首次部署必须创建 `.env`；当前仓库默认没有 `.env`。
- 容器健康不代表模型可用。没有 LLM/Embedding 配置时，文档解析和问答会失败或不可用。
- `full` profile 会启动很多侧车服务，但部分能力需要显式启用或在 UI 中配置。
- 默认数据库迁移会在 app 启动时自动执行；迁移失败会记录 warning 并继续启动，需查看日志确认。
- Langfuse 只有在 `LANGFUSE_PUBLIC_KEY` 与 `LANGFUSE_SECRET_KEY` 同时设置时才会启用上报。
- Neo4j 启动后还需要 `NEO4J_ENABLE=true` 和知识库图谱抽取配置。
- MinIO 启动后还需要 `STORAGE_TYPE=minio` 或在对象存储配置中选择它。
- SearXNG 启动后需要在 Web UI 中新增网页搜索提供者。
- MCP Server 启动后需要有效 `WEKNORA_API_KEY` 才能访问 WeKnora API。

## 12. 主要参考资料

- 本仓库 `README_CN.md`、`README.md`
- 本仓库 `docker-compose.yml`、`.env.example`
- 本仓库 `docs/BUILTIN_MODELS.md`
- 本仓库 `docs/Langfuse集成.md`
- 本仓库 `docs/KnowledgeGraph.md`
- 本仓库 `docs/RBAC说明.md`
- 本仓库 `docs/CHUNKING.md`
- 本仓库 `docs/api/README.md`
- 本仓库 `docreader/README.md`
- 本仓库 `cli/README.md`
- 公开仓库：[https://github.com/Tencent/WeKnora](https://github.com/Tencent/WeKnora)
- 官网：[https://weknora.weixin.qq.com](https://weknora.weixin.qq.com)
