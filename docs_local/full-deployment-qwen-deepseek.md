# WeKnora 推荐全量部署指南（MinIO + Qdrant + Qwen/DeepSeek）

本文档面向在当前项目目录下部署一套“推荐全量”WeKnora 知识库服务。默认场景是单机 Docker Compose 部署，使用服务器内网 IP 或本机 `localhost` 访问；公网域名、HTTPS、反向代理作为可选增强项。

默认部署策略：

- 启动范围：`docker compose --profile full up -d`
- 文件存储：MinIO
- 向量检索：Qdrant
- 默认大模型：Qwen / 阿里云 DashScope
- 额外大模型：DeepSeek API
- 可观测性：自建 Langfuse，随 `full` profile 启动
- 网络搜索：SearXNG，随 `full` profile 启动

> 注意：本文不会要求把 API Key 写进仓库。模型密钥写入项目根目录 `.env`，`config/builtin_models.yaml` 只引用环境变量。

## 1. 推荐全量部署包含哪些组件

`docker-compose.yml` 的默认核心服务会启动：

| 组件 | 镜像 | 容器名 | 默认端口 | 用途 |
|---|---|---|---|---|
| frontend | `wechatopenai/weknora-ui:${WEKNORA_VERSION:-latest}` | `WeKnora-frontend` | `9750 -> 80` | Web UI 与 Nginx 反向代理 |
| app | `wechatopenai/weknora-app:${WEKNORA_VERSION:-latest}` | `WeKnora-app` | `9751 -> 8080` | Go 后端 API、RAG、Agent、任务队列 worker |
| docreader | `wechatopenai/weknora-docreader:${WEKNORA_VERSION:-latest}` | `WeKnora-docreader` | 仅 compose 网络内 `50051` | 文档解析 gRPC 服务 |
| postgres | `paradedb/paradedb:v0.22.2-pg17` | `WeKnora-postgres` | 不暴露到宿主机 | 主数据库 |
| redis | `redis:7.0-alpine` | `WeKnora-redis` | 不暴露到宿主机 | Asynq 队列、流管理、缓存 |

`--profile full` 会额外启动：

| 组件 | 镜像 | 容器名 | 默认端口 | 用途 |
|---|---|---|---|---|
| minio | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | `WeKnora-minio` | `9753 -> 9000`, `9754 -> 9001` | 对象存储 |
| neo4j | `neo4j:2025.10.1` | `WeKnora-neo4j` | `9755 -> 7474`, `9756 -> 7687` | 知识图谱 / GraphRAG |
| qdrant | `qdrant/qdrant:v1.16.2` | `WeKnora-qdrant` | `9757 -> 6333`, `9758 -> 6334` | 向量数据库 |
| searxng-init | `busybox:1.36` | `WeKnora-searxng-init` | 无 | 初始化 SearXNG 配置卷 |
| searxng | `searxng/searxng:latest` | `WeKnora-searxng` | `127.0.0.1:9752 -> 8080` | 本地聚合搜索引擎 |
| dex | `dexidp/dex:latest` | `dex` | `9766 -> 5556` | OIDC 测试/演示身份源 |
| langfuse-db-init | `paradedb/paradedb:v0.22.2-pg17` | `WeKnora-langfuse-db-init` | 无 | 在 Postgres 内创建 Langfuse 数据库 |
| langfuse-clickhouse | `clickhouse/clickhouse-server:24.8` | `WeKnora-langfuse-clickhouse` | 不暴露 | Langfuse 事件存储 |
| langfuse-minio | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | `WeKnora-langfuse-minio` | `9767 -> 9000`, `9768 -> 9001` | Langfuse 专用对象存储 |
| langfuse-worker | `langfuse/langfuse-worker:3` | `WeKnora-langfuse-worker` | 无 | Langfuse 后台 worker |
| langfuse-web | `langfuse/langfuse:3` | `WeKnora-langfuse-web` | `9769 -> 3000` | Langfuse Web UI |
| mcp | 本地构建 `./mcp-server` | `WeKnora-mcp` | `9770 -> 8000` | WeKnora MCP Server |
| sandbox | `wechatopenai/weknora-sandbox:${WEKNORA_VERSION:-latest}` | `WeKnora-sandbox` | 无 | Agent Skills 沙箱镜像拉取/构建占位，不是常驻业务服务 |

以下组件在 `docker-compose.yml` 中存在，但不属于本文默认“推荐全量”：

| 组件 | profile | 说明 |
|---|---|---|
| Milvus | `milvus` | Qdrant/Postgres 的替代向量数据库，默认不启用；宿主机端口 `9759 -> 19530`、`9760 -> 9091` |
| Weaviate | `weaviate` | Qdrant/Postgres 的替代向量数据库，默认不启用；宿主机端口 `9761 -> 8080`、`9762 -> 50051` |
| Doris FE/BE | `doris` | Apache Doris 向量后端，默认不启用；宿主机端口 `9763 -> 8030`、`9764 -> 9030`、`9765 -> 8040` |
| OpenDataLoader hybrid | `odl-hybrid` | 更重的文档解析后端，本地构建，默认不启用 |

## 2. 环境要求

建议配置：

- Linux x86_64 / arm64 服务器
- Docker Engine
- Docker Compose v2（优先 `docker compose`）
- 8 CPU / 16 GB 内存起步；启用 Langfuse、Neo4j、Qdrant 后建议 16 CPU / 32 GB 内存以上
- 可访问外网，至少需要访问：
  - Docker 镜像仓库
  - 阿里云 DashScope API
  - DeepSeek API

检查命令：

```bash
docker version
docker compose version
docker info
```

## 3. 准备 `.env`

在项目根目录执行：

```bash
cp .env.example .env
```

生产部署不要直接保留 `.env.example` 里的占位值。至少修改以下配置。

### 3.1 基础服务配置

```bash
WEKNORA_VERSION=latest
GIN_MODE=release
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN

FRONTEND_PORT=9750
APP_PORT=9751
APP_HOST=app
APP_BACKEND_PORT=8080
APP_SCHEME=http
```

端口说明：

- `FRONTEND_PORT=9750` 是宿主机访问 Web UI 的端口。
- `APP_PORT=9751` 是宿主机直连后端 API 的端口。
- `APP_BACKEND_PORT=8080` 是 frontend 容器内 Nginx 代理到 app 容器的内部端口，不要随宿主机端口一起改。

访问地址为 `http://服务器IP:9750`。

### 3.2 数据库与 Redis

```bash
DB_DRIVER=postgres
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=请替换为强密码
DB_NAME=WeKnora

STREAM_MANAGER_TYPE=redis
REDIS_ADDR=redis:6379
REDIS_PASSWORD=请替换为强密码
REDIS_DB=0
REDIS_PREFIX=stream:
```

`DB_PASSWORD` 和 `REDIS_PASSWORD` 会被多个容器复用。可包含特殊字符，但部署排障会更复杂；如果没有强约束，建议使用字母、数字、`_`、`-` 组合。

### 3.3 MinIO 文件存储

本文默认使用 MinIO，而不是本地文件存储：

```bash
STORAGE_TYPE=minio
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY_ID=请替换为MinIO账号
MINIO_SECRET_ACCESS_KEY=请替换为MinIO强密码
MINIO_BUCKET_NAME=weknora
MINIO_USE_SSL=false
```

说明：

- `MINIO_ENDPOINT=minio:9000` 是容器内访问地址，适合单机内网部署。
- app 启动时会检查 bucket，不存在会自动创建。
- 如果要接入企业微信、飞书、Slack 等 IM 平台，MinIO 生成的文件链接需要公网可达；这时应改成公网 MinIO 域名并配置 HTTPS。

### 3.4 Qdrant 向量数据库

本文默认使用 Qdrant 作为向量后端：

```bash
RETRIEVE_DRIVER=qdrant
QDRANT_HOST=qdrant
QDRANT_PORT=6334
QDRANT_REST_PORT=9757
QDRANT_GRPC_HOST_PORT=9758
QDRANT_COLLECTION=weknora_embeddings
QDRANT_USE_TLS=false
QDRANT_API_KEY=
```

说明：

- Qdrant 容器随 `--profile full` 启动。
- app 使用 Qdrant gRPC 端口 `6334`。
- `QDRANT_PORT=6334` 是容器内连接端口；宿主机暴露端口使用 `QDRANT_GRPC_HOST_PORT=9758`，不要混用。
- 启动了 Qdrant 不代表 app 自动使用它；必须设置 `RETRIEVE_DRIVER=qdrant`。

### 3.5 Neo4j 知识图谱

`full` profile 会启动 Neo4j。若要启用图谱能力：

```bash
NEO4J_ENABLE=true
ENABLE_GRAPH_RAG=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=请替换为Neo4j强密码
```

如果暂时不用图谱，可以保留容器但关闭功能：

```bash
NEO4J_ENABLE=false
ENABLE_GRAPH_RAG=false
```

### 3.6 安全密钥

`SYSTEM_AES_KEY` 必须是 32 字节字符串。可以用以下命令生成：

```bash
openssl rand -hex 16
```

建议配置：

```bash
JWT_SECRET=用 openssl rand -hex 32 生成
TENANT_AES_KEY=用 openssl rand -hex 16 生成
SYSTEM_AES_KEY=用 openssl rand -hex 16 生成，必须刚好32字符
CRYPTO_MASTER_KEY=
CRYPTO_SALT=
```

说明：

- `JWT_SECRET` 控制登录态签名，生产环境必须固定。
- `SYSTEM_AES_KEY` 用于模型 API Key、MCP 凭据、数据源凭据等敏感字段加密。上线后不要随意更换，否则旧密文无法解密。
- `CRYPTO_MASTER_KEY` / `CRYPTO_SALT` 不设置时，应用会自动生成并持久化到 `data-files` volume。生产上也可以显式配置并妥善保管。

### 3.7 注册与系统管理员

如果是首次部署，可以先允许注册：

```bash
DISABLE_REGISTRATION=false
WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL=你的管理员邮箱
```

流程：

1. 启动服务。
2. 用该邮箱注册第一个账号。
3. 重启 app 或等待下次启动 bootstrap。
4. 该用户会在没有现有系统管理员时被提升为 system admin。
5. 完成后建议关闭公开注册：

```bash
DISABLE_REGISTRATION=true
```

### 3.8 Langfuse

`full` profile 会启动自建 Langfuse。为了避免 `.env.example` 中的占位 key 导致 app 尝试用假凭据上报，首次启动前建议先显式关闭 app 侧上报：

```bash
LANGFUSE_ENABLED=false
LANGFUSE_PUBLIC_KEY=
LANGFUSE_SECRET_KEY=
LANGFUSE_HOST=http://langfuse-web:3000
```

如果希望首次启动就自动初始化 Langfuse，并让 app 直接上报，可设置：

```bash
LANGFUSE_ENABLED=true
LANGFUSE_HOST=http://langfuse-web:3000

LANGFUSE_INIT_ORG_ID=weknora
LANGFUSE_INIT_ORG_NAME=WeKnora
LANGFUSE_INIT_PROJECT_ID=weknora
LANGFUSE_INIT_PROJECT_NAME=WeKnora
LANGFUSE_INIT_PROJECT_PUBLIC_KEY=pk-lf-weknora-local-请替换
LANGFUSE_INIT_PROJECT_SECRET_KEY=sk-lf-weknora-local-请替换
LANGFUSE_INIT_USER_EMAIL=admin@example.com
LANGFUSE_INIT_USER_NAME=Admin
LANGFUSE_INIT_USER_PASSWORD=请替换为强密码

LANGFUSE_PUBLIC_KEY=pk-lf-weknora-local-请替换
LANGFUSE_SECRET_KEY=sk-lf-weknora-local-请替换
LANGFUSE_SALT=用 openssl rand -base64 32 生成
LANGFUSE_ENCRYPTION_KEY=用 openssl rand -hex 32 生成
LANGFUSE_NEXTAUTH_SECRET=用 openssl rand -base64 32 生成
LANGFUSE_NEXTAUTH_URL=http://服务器IP:9769
LANGFUSE_WEB_PORT=9769
LANGFUSE_MINIO_S3_PORT=9767
LANGFUSE_MINIO_CONSOLE_PORT=9768
LANGFUSE_TELEMETRY_ENABLED=false
```

### 3.9 SearXNG

`full` profile 会启动 SearXNG，并默认只绑定到 `127.0.0.1:9752`：

```bash
SEARXNG_PORT=9752
SEARXNG_SECRET=用 openssl rand -hex 32 生成
```

不建议将 SearXNG 裸露到公网。如果确实要开放：

```bash
SEARXNG_BIND=0.0.0.0
```

同时必须更换 `SEARXNG_SECRET` 并增加外部访问控制。

### 3.10 MCP Server

`full` profile 会启动 `WeKnora-mcp`。它需要 WeKnora API Key：

```bash
MCP_PORT=9770
WEKNORA_API_KEY=
MCP_SERVER_AUTH_TOKEN=用 openssl rand -hex 32 生成
```

`MCP_SERVER_AUTH_TOKEN` 不能为空；HTTP MCP 客户端需要发送 `Authorization: Bearer <token>` 或 `X-MCP-Auth-Token`。

首次启动时 `WEKNORA_API_KEY` 可以留空。等你在 Web UI 中创建租户 API Key 后，再把 `WEKNORA_API_KEY` 填入 `.env` 并重启 MCP：

```bash
docker compose --profile full up -d mcp
```

## 4. 配置 Qwen 和 DeepSeek 内置模型

推荐用声明式内置模型配置，所有租户可见，且密钥不会在前端暴露。

### 4.1 创建 `config/builtin_models.yaml`

在项目根目录执行：

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

把文件内容替换为：

```yaml
builtin_models:
  - id: builtin-qwen-chat
    name: ${QWEN_CHAT_MODEL}
    type: KnowledgeQA
    source: remote
    description: 默认 Qwen 对话模型（DashScope OpenAI 兼容模式）
    is_default: true
    status: active
    parameters:
      base_url: ${QWEN_BASE_URL}
      api_key: ${QWEN_API_KEY}
      provider: aliyun

  - id: builtin-qwen-embedding
    name: ${QWEN_EMBEDDING_MODEL}
    type: Embedding
    source: remote
    description: 默认 Qwen 文本向量模型
    is_default: true
    status: active
    parameters:
      base_url: ${QWEN_BASE_URL}
      api_key: ${QWEN_API_KEY}
      provider: aliyun
      embedding_parameters:
        dimension: 1024
        truncate_prompt_tokens: 0

  - id: builtin-qwen-rerank
    name: ${QWEN_RERANK_MODEL}
    type: Rerank
    source: remote
    description: Qwen Rerank 模型
    is_default: true
    status: active
    parameters:
      base_url: ${QWEN_RERANK_BASE_URL}
      api_key: ${QWEN_API_KEY}
      provider: aliyun

  - id: builtin-deepseek-chat
    name: ${DEEPSEEK_CHAT_MODEL}
    type: KnowledgeQA
    source: remote
    description: DeepSeek Chat 模型
    is_default: false
    status: active
    parameters:
      base_url: ${DEEPSEEK_BASE_URL}
      api_key: ${DEEPSEEK_API_KEY}
      provider: deepseek

  - id: builtin-deepseek-reasoner
    name: ${DEEPSEEK_REASONER_MODEL}
    type: KnowledgeQA
    source: remote
    description: DeepSeek Reasoner 模型
    is_default: false
    status: active
    parameters:
      base_url: ${DEEPSEEK_BASE_URL}
      api_key: ${DEEPSEEK_API_KEY}
      provider: deepseek
```

### 4.2 在 `.env` 中加入模型变量

```bash
# Qwen / DashScope
QWEN_API_KEY=sk-你的DashScopeKey
QWEN_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
QWEN_CHAT_MODEL=qwen-plus
QWEN_EMBEDDING_MODEL=text-embedding-v4
QWEN_RERANK_MODEL=qwen3-rerank
QWEN_RERANK_BASE_URL=https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank

# DeepSeek
DEEPSEEK_API_KEY=sk-你的DeepSeekKey
DEEPSEEK_BASE_URL=https://api.deepseek.com/v1
DEEPSEEK_CHAT_MODEL=deepseek-chat
DEEPSEEK_REASONER_MODEL=deepseek-reasoner
```

说明：

- WeKnora 当前代码中 Qwen 使用 `provider: aliyun`。
- Qwen Chat 和文本 Embedding 使用 DashScope OpenAI 兼容地址 `/compatible-mode/v1`。
- Qwen Rerank 使用 DashScope rerank 专用 endpoint。
- DeepSeek 在项目中只注册为 `KnowledgeQA` provider，不作为 Embedding/Rerank 使用。
- Embedding 维度会影响向量索引。已有知识库导入后不要随意更换 embedding 模型或维度；如需更换，重新解析/重建索引。

### 4.3 让 app 容器加载 `builtin_models.yaml`

推荐新增 `docker-compose.override.yml`，避免直接修改上游 `docker-compose.yml`：

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

Docker Compose 会自动合并 `docker-compose.override.yml`。

如果你不想使用 override，也可以直接取消 `docker-compose.yml` 中 app 服务下这行注释：

```yaml
- ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

## 5. 启动服务

### 5.1 拉取并启动推荐全量服务

```bash
docker compose --profile full up --pull always -d
```

如果网络环境无法拉取镜像，可先手动拉取或使用本地镜像：

```bash
docker compose --profile full pull
docker compose --profile full up -d
```

项目脚本也可以启动核心服务，但它默认不会带上 `full` profile。本文建议直接使用 Docker Compose 命令，避免 profile 范围不明确。

### 5.2 查看容器状态

```bash
docker compose --profile full ps
```

关键容器应为 `running` 或 `healthy`：

- `WeKnora-frontend`
- `WeKnora-app`
- `WeKnora-docreader`
- `WeKnora-postgres`
- `WeKnora-redis`
- `WeKnora-minio`
- `WeKnora-qdrant`
- `WeKnora-neo4j`
- `WeKnora-langfuse-web`

### 5.3 验证后端健康

```bash
curl -f http://localhost:9751/health
```

期望返回：

```json
{"status":"ok"}
```

如果服务器只开放前端端口，也可以访问：

```bash
curl -f http://localhost:9750/health
```

前端 Nginx 会代理到后端。

### 5.4 验证内置模型导入

```bash
docker compose logs app | grep -E "Built-in model|builtin models|Load builtin"
```

正常情况下应看到类似日志：

```text
Built-in model upserted: id=builtin-qwen-chat name=qwen-plus type=KnowledgeQA
Built-in model upserted: id=builtin-qwen-embedding name=text-embedding-v4 type=Embedding
Built-in model upserted: id=builtin-qwen-rerank name=qwen3-rerank type=Rerank
Built-in model upserted: id=builtin-deepseek-chat name=deepseek-chat type=KnowledgeQA
```

如果没有看到导入日志，优先检查：

1. `docker-compose.override.yml` 是否存在并挂载了 `config/builtin_models.yaml`。
2. `config/builtin_models.yaml` YAML 格式是否正确。
3. `.env` 变量是否传入 app 容器：

```bash
docker compose exec app env | grep -E "QWEN_|DEEPSEEK_"
```

## 6. 首次初始化与基本使用

### 6.1 访问 Web UI

打开：

```text
http://服务器IP:9750/
```

如果你之后修改了 `FRONTEND_PORT`，例如 `9780`：

```text
http://服务器IP:9780/
```

### 6.2 注册管理员账号

如果 `.env` 中设置：

```bash
DISABLE_REGISTRATION=false
WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL=admin@example.com
```

则用 `admin@example.com` 注册。注册后重启 app：

```bash
docker compose restart app
```

确认 bootstrap 日志：

```bash
docker compose logs app | grep bootstrap
```

### 6.3 创建知识库

在 Web UI 中创建知识库时建议：

- Summary / Chat 模型：`builtin-qwen-chat`
- Embedding 模型：`builtin-qwen-embedding`
- Rerank 模型：`builtin-qwen-rerank`
- 存储：默认 MinIO
- 向量后端：默认 Qdrant
- 如需知识图谱：启用 Wiki / Graph 相关设置，并确认 Neo4j 已启用

上传文档后，任务链路大致为：

```text
上传文件 -> app 保存到 MinIO -> docreader 解析 -> app 分块 -> 写入 Qdrant -> 后处理 summary/question/graph/wiki -> 可检索问答
```

## 7. 常用运维命令

### 7.1 查看日志

```bash
docker compose logs -f app
docker compose logs -f docreader
docker compose logs -f frontend
docker compose logs -f qdrant
docker compose logs -f minio
```

### 7.2 重启服务

```bash
docker compose --profile full restart app
docker compose --profile full restart docreader
docker compose --profile full restart frontend
```

### 7.3 停止服务

```bash
docker compose --profile full down
```

只停止容器但保留数据卷。不要随意加 `-v`，否则会删除数据库、MinIO、Qdrant、Langfuse 等数据卷。

### 7.4 升级镜像

```bash
docker compose --profile full pull
docker compose --profile full up -d
```

升级前建议备份数据库和数据卷。

## 8. 备份与恢复

### 8.1 备份 Postgres

```bash
docker exec WeKnora-postgres pg_dump -U postgres WeKnora > weknora-postgres-$(date +%F).sql
```

如果你改了 `DB_USER` / `DB_NAME`，同步替换命令中的用户名和数据库名。

恢复示例：

```bash
cat weknora-postgres-YYYY-MM-DD.sql | docker exec -i WeKnora-postgres psql -U postgres WeKnora
```

### 8.2 备份对象存储和向量库

Compose 使用 named volumes。先查看实际卷名：

```bash
docker volume ls | grep -E "postgres|data-files|minio|qdrant|neo4j|langfuse"
```

建议至少备份：

- Postgres：业务元数据、用户、模型配置、会话等
- MinIO：上传的原始文件、解析产物
- Qdrant：向量索引
- Neo4j：图谱数据
- `data-files`：本地加密状态、运行态文件
- Langfuse ClickHouse / MinIO：可观测性数据

如果可以接受重建索引，Qdrant/Neo4j 可通过重新解析文档恢复；但 Postgres 和 MinIO 必须重点备份。

## 9. 常见问题排查

### 9.1 app 启动失败：`missing MinIO configuration`

检查 `.env`：

```bash
STORAGE_TYPE=minio
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY_ID=...
MINIO_SECRET_ACCESS_KEY=...
MINIO_BUCKET_NAME=weknora
```

`MINIO_BUCKET_NAME` 不能为空。

### 9.2 Qdrant 启动了，但检索没有用 Qdrant

检查：

```bash
docker compose exec app env | grep RETRIEVE_DRIVER
```

必须是：

```bash
RETRIEVE_DRIVER=qdrant
```

### 9.3 内置模型没有出现

检查三件事：

1. `config/builtin_models.yaml` 是否存在。
2. `docker-compose.override.yml` 是否挂载该文件。
3. app 日志是否有 YAML parse warning：

```bash
docker compose logs app | grep -i builtin
```

### 9.4 Qwen 调用失败

检查：

```bash
docker compose exec app env | grep QWEN
```

确认：

- `QWEN_API_KEY` 有值。
- `QWEN_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1`
- `QWEN_RERANK_BASE_URL=https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank`
- 服务器能访问 DashScope。

### 9.5 DeepSeek 调用失败

检查：

```bash
docker compose exec app env | grep DEEPSEEK
```

确认：

- `DEEPSEEK_API_KEY` 有值。
- `DEEPSEEK_BASE_URL=https://api.deepseek.com/v1`
- 服务器能访问 DeepSeek API。

### 9.6 Langfuse 报认证错误

如果 `.env` 里保留了 `.env.example` 的占位 key，例如 `pk-lf-xxxxxxxx`，app 会尝试用假凭据上报。

先关闭：

```bash
LANGFUSE_ENABLED=false
LANGFUSE_PUBLIC_KEY=
LANGFUSE_SECRET_KEY=
```

重启 app：

```bash
docker compose restart app
```

等 Langfuse UI 初始化完成并生成真实 key 后，再填回 `.env`。

### 9.7 MCP Server 401

`WeKnora-mcp` 需要租户 API Key。首次启动可以先不管。生成 API Key 后设置：

```bash
WEKNORA_API_KEY=sk-...
```

重启：

```bash
docker compose --profile full up -d mcp
```

## 10. 公网域名和 HTTPS 可选增强

默认文档按单机内网/IP 写。如果要公网部署，建议：

1. 用 Nginx / Caddy / Traefik 放在 Docker Compose 前面。
2. 只暴露 80/443；不要直接暴露 Postgres、Redis、Qdrant、Neo4j Bolt、ClickHouse。
3. 给 WeKnora Web UI 配置 HTTPS 域名，例如 `https://kb.example.com`。
4. 如果使用 IM、Embed Widget、外部 MCP 客户端，需要设置公网可达地址：
   - `APP_EXTERNAL_URL=https://kb.example.com`
   - MinIO 使用公网 HTTPS endpoint 或改用云对象存储
   - `LANGFUSE_NEXTAUTH_URL=https://langfuse.example.com`
5. SearXNG、MinIO Console、Langfuse、Neo4j Browser 如需公网访问，必须加认证和访问控制。

## 11. 安全检查清单

上线前至少检查：

- [ ] `.env` 不提交到 Git。
- [ ] `DB_PASSWORD` 已替换。
- [ ] `REDIS_PASSWORD` 已替换。
- [ ] `MINIO_ACCESS_KEY_ID` / `MINIO_SECRET_ACCESS_KEY` 已替换。
- [ ] `NEO4J_PASSWORD` 已替换。
- [ ] `JWT_SECRET` 已设置。
- [ ] `SYSTEM_AES_KEY` 已设置且刚好 32 字符。
- [ ] `LANGFUSE_SALT` / `LANGFUSE_ENCRYPTION_KEY` / `LANGFUSE_NEXTAUTH_SECRET` 已替换。
- [ ] `SEARXNG_SECRET` 已替换。
- [ ] Qwen / DeepSeek API Key 只写在 `.env`，没有写进 Markdown 或 Git。
- [ ] 生产环境关闭公开注册：`DISABLE_REGISTRATION=true`。
- [ ] 没有把数据库、Redis、Qdrant、ClickHouse 暴露到公网。

## 12. 参考

- 项目 Docker Compose：`docker-compose.yml`
- 内置模型配置：`docs/BUILTIN_MODELS.md`
- Chunking 配置：`docs/CHUNKING.md`
- RBAC：`docs/RBAC说明.md`
- Langfuse：`docs/Langfuse集成.md`
- Qwen / DashScope OpenAI 兼容模式：https://www.alibabacloud.com/help/en/model-studio/compatibility-of-openai-with-dashscope
- Qwen Embedding：https://www.alibabacloud.com/help/en/model-studio/text-embedding-synchronous-api
- DeepSeek API：https://api-docs.deepseek.com/
