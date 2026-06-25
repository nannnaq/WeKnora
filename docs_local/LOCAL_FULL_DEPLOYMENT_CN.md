# WeKnora 本地全量部署启动文档

本文说明如何在本地使用 Docker Compose 启动 WeKnora 标准版全量栈。文档基于当前仓库 `0.6.2`（本地提交 `122408b2`）整理。

这里的“全量部署”指启动 `docker-compose.yml` 中 `full` profile 覆盖的服务：

- 核心服务：`frontend`、`app`、`docreader`、`postgres`、`redis`
- 可选能力侧车：`minio`、`neo4j`、`qdrant`、`langfuse-*`、`searxng`、`dex`、`mcp`、`sandbox`

注意：`full` profile 不包含 `milvus`、`weaviate`、`doris`、`odl-hybrid`。这些是额外可选组件，本文在附录说明。

## 1. 部署前准备

### 1.1 推荐机器配置

本地核心服务可以在较小机器上运行，但 `full` profile 会额外启动 Langfuse、ClickHouse、MinIO、Neo4j、Qdrant、SearXNG 等组件。建议：

- CPU：4 核以上，推荐 8 核。
- 内存：8 GB 可尝试，推荐 16 GB 以上。
- 磁盘：至少 20 GB 可用空间，推荐 50 GB 以上。
- 网络：首次启动需要拉取多个 Docker 镜像。

如果只是快速体验核心问答，不需要 Langfuse、Neo4j、MinIO、Qdrant 等，可以使用默认核心部署：

```bash
docker compose up -d
```

本文后续按全量 profile 编写。

## 3. 创建环境变量文件

当前仓库默认没有 `.env`。由于 `docker-compose.yml` 的 `app` 服务使用 `env_file: .env`，正式启动前必须先创建：

```bash
cp .env.example .env
```

建议先打开 `.env` 检查和修改关键项：

```bash
vim .env
```

### 3.1 本地试用必看配置

下面这些变量会直接影响能否启动和使用：

```bash
# 镜像版本：latest 为稳定版，main 为最新开发版
WEKNORA_VERSION=latest

# 后端运行模式：release 默认关闭 Swagger
GIN_MODE=release

# 数据库
DB_DRIVER=postgres
DB_USER=postgres
DB_PASSWORD=postgres123!@#
DB_NAME=WeKnora

# Redis
REDIS_PASSWORD=redis123!@#
REDIS_DB=0

# 检索后端。默认使用 postgres/pgvector
RETRIEVE_DRIVER=postgres

# 文件存储。默认 local，文件在 data-files volume 中
STORAGE_TYPE=local

# 前端和后端宿主机端口
FRONTEND_PORT=80
APP_PORT=8080

# JWT secret，本地可先保留；生产必须换强随机值
JWT_SECRET=weknora-jwt-secret

# 系统敏感字段加密密钥，SYSTEM_AES_KEY 必须为 32 字节
TENANT_AES_KEY=weknorarag-api-key-secret-secret
SYSTEM_AES_KEY=weknora-system-aes-key-32bytes!!
```

生产或多人共享环境不要使用模板里的默认密码。可以用下面命令生成随机值：

```bash
openssl rand -base64 32
openssl rand -hex 32
```

### 3.2 Langfuse 配置建议

`.env.example` 中 `LANGFUSE_PUBLIC_KEY` 和 `LANGFUSE_SECRET_KEY` 是占位值。如果保留占位值，app 会尝试启用 Langfuse 上报，但 key 无效。建议二选一。

方案 A：首次启动先不启用上报，等 Langfuse Web 初始化后再填 key：

```bash
LANGFUSE_PUBLIC_KEY=
LANGFUSE_SECRET_KEY=
LANGFUSE_HOST=http://langfuse-web:3000
```

启动全量栈后访问 `http://localhost:3000`，注册 Langfuse 管理员，在项目设置里生成 Public/Secret Key，再回填 `.env`：

```bash
LANGFUSE_SECRET_KEY=sk-lf-b0b58031-3b53-42b5-8bdf-df096dec7939
LANGFUSE_PUBLIC_KEY=pk-lf-2ae1ca93-8fe6-4c22-b4ed-e8f02cec5e7b
LANGFUSE_BASE_URL=http://localhost:3000
```

然后重启 app：

```bash
docker compose --profile full up -d app
```

方案 B：使用 Langfuse 自动初始化。适合本地一次性拉起：

```bash
LANGFUSE_HOST=http://langfuse-web:3000
LANGFUSE_PUBLIC_KEY=pk-lf-weknora-local
LANGFUSE_SECRET_KEY=sk-lf-weknora-local-change-me

LANGFUSE_INIT_ORG_ID=weknora
LANGFUSE_INIT_ORG_NAME=WeKnora
LANGFUSE_INIT_PROJECT_ID=weknora
LANGFUSE_INIT_PROJECT_NAME=WeKnora
LANGFUSE_INIT_PROJECT_PUBLIC_KEY=pk-lf-weknora-local
LANGFUSE_INIT_PROJECT_SECRET_KEY=sk-lf-weknora-local-change-me
LANGFUSE_INIT_USER_EMAIL=admin@example.com
LANGFUSE_INIT_USER_NAME=Admin
LANGFUSE_INIT_USER_PASSWORD=change-me-please
```

生产环境必须替换 Langfuse 的 `SALT`、`ENCRYPTION_KEY`、`NEXTAUTH_SECRET` 和相关密码：

```bash
LANGFUSE_SALT=$(openssl rand -base64 32)
LANGFUSE_ENCRYPTION_KEY=$(openssl rand -hex 32)
LANGFUSE_NEXTAUTH_SECRET=$(openssl rand -base64 32)
```

### 3.3 是否启用知识图谱

`full` profile 会启动 Neo4j，但 WeKnora 后端默认不一定启用图谱逻辑。需要图谱时设置：

```bash
ENABLE_GRAPH_RAG=true
NEO4J_ENABLE=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=password
```

之后还需要在知识库设置中启用实体/关系抽取或图谱相关索引策略。

### 3.4 是否使用 MinIO 作为主文件存储

`full` profile 会启动 MinIO，但默认 `STORAGE_TYPE=local`，文件仍存在 app 的 `data-files` volume 中。

如果希望全量栈使用 MinIO：

```bash
STORAGE_TYPE=minio
MINIO_ACCESS_KEY_ID=minioadmin
MINIO_SECRET_ACCESS_KEY=minioadmin
MINIO_BUCKET_NAME=weknora
MINIO_ENDPOINT=minio:9000
MINIO_PORT=9000
MINIO_CONSOLE_PORT=9001
```

如果需要在 IM 平台展示图片，MinIO endpoint 必须是客户端或 IM 平台服务器可访问的公网或内网地址，不能只写 Docker 内部服务名。

### 3.5 是否使用 Qdrant

`full` profile 会启动 Qdrant，但默认检索后端仍是 PostgreSQL：

```bash
RETRIEVE_DRIVER=postgres
```

如果希望默认使用 Qdrant，可以改为：

```bash
RETRIEVE_DRIVER=qdrant
QDRANT_HOST=qdrant
QDRANT_PORT=6334
QDRANT_COLLECTION=weknora_embeddings
```

也可以启动后在 Web UI 的“设置 -> 向量库”里新增和测试 Qdrant，再让知识库绑定该向量库。

### 3.6 MCP Server API Key

`full` profile 会启动 `mcp` 容器，但它需要 `WEKNORA_API_KEY` 才能访问 WeKnora API。首次启动前可以留空：

```bash
WEKNORA_API_KEY=
MCP_PORT=8082
```

待 Web UI 中注册用户并生成 API Key 后，再回填：

```bash
WEKNORA_API_KEY=sk-7Nuu5Xmr0rIT2yW88IHHZNT3oJooS3MMRi-rTknWkzqcohxB
```

然后重启 MCP 容器：

```bash
docker compose --profile full up -d mcp
```

## 4. 模型配置

容器全部启动不代表文档能上传和问答。WeKnora 至少需要：

- 一个 LLM / KnowledgeQA 模型，用于生成答案。
- 一个 Embedding 模型，用于文档向量化和检索。
- 可选 Rerank 模型，用于提升召回排序。
- 可选 VLM/ASR 模型，用于图片、扫描件、音频场景。

有两种配置方式。

### 4.1 方式一：启动后在 Web UI 配置

这是本地部署最直观的方式：

1. 启动服务并访问 `http://localhost`。
2. 注册或登录。
3. 进入“设置 -> 模型”。
4. 添加 LLM、Embedding、Rerank 等模型。
5. 创建知识库时选择这些模型。

这种方式不需要修改 `docker-compose.yml`。

### 4.2 方式二：用 YAML 声明式内置模型

适合团队部署或希望配置可复现的环境。

复制示例文件：

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

编辑 `config/builtin_models.yaml`，示例：

```yaml
builtin_models:
  - id: builtin-llm-default
    type: KnowledgeQA
    source: remote
    is_default: true
    name: ${LLM_MODEL_NAME}
    parameters:
      base_url: ${LLM_BASE_URL}
      api_key: ${LLM_API_KEY}
      provider: ${LLM_PROVIDER}

  - id: builtin-embedding-default
    type: Embedding
    source: remote
    is_default: true
    name: ${EMBEDDING_MODEL_NAME}
    parameters:
      base_url: ${EMBEDDING_BASE_URL}
      api_key: ${EMBEDDING_API_KEY}
      provider: ${EMBEDDING_PROVIDER}
      embedding_parameters:
        dimension: 1536
        truncate_prompt_tokens: 0
```

在 `.env` 中写入真实模型变量：

```bash
LLM_MODEL_NAME=gpt-4o-mini
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=sk-...
LLM_PROVIDER=openai

EMBEDDING_MODEL_NAME=text-embedding-3-small
EMBEDDING_BASE_URL=https://api.openai.com/v1
EMBEDDING_API_KEY=sk-...
EMBEDDING_PROVIDER=openai
```

然后给 app 挂载该文件。推荐新建 `docker-compose.override.yml`，避免直接改主 compose：

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

启动或重启 app 后，后端会在启动期读取 YAML 并写入内置模型：

```bash
docker compose --profile full up -d app
docker compose logs app | grep -E 'Built-in models? config|Built-in model'
```

注意：

- Embedding 的 `dimension` 必须与模型真实输出维度一致。
- OpenAI-compatible 服务通常使用 `provider: openai` 或项目支持的对应 provider。
- 如果 `${ENV}` 变量未设置，系统会保留字面值，后续模型调用会暴露配置错误。

## 5. 拉取镜像并启动全量服务

建议先拉取镜像：

```bash
docker compose --profile full pull
```

启动全量栈：

```bash
docker compose --profile full up -d
```

如果你正在从源码构建镜像，而不是拉取官方镜像：

```bash
docker compose --profile full up -d --build
```

首次启动 Langfuse 和 ClickHouse 可能需要 1 到 2 分钟完成迁移和初始化。

## 6. 检查服务状态

查看所有服务：

```bash
docker compose --profile full ps
```

核心服务至少应包括：

```text
WeKnora-frontend
WeKnora-app
WeKnora-docreader
WeKnora-postgres
WeKnora-redis
```

全量 profile 还会包含：

```text
WeKnora-minio
WeKnora-neo4j
WeKnora-qdrant
WeKnora-searxng
WeKnora-langfuse-web
WeKnora-langfuse-worker
WeKnora-langfuse-clickhouse
WeKnora-langfuse-minio
WeKnora-mcp
dex
```

`WeKnora-sandbox` 是镜像准备项，命令为 `true`，不是常驻服务；看到它退出不代表核心服务失败。

查看关键日志：

```bash
docker compose logs -f app docreader postgres redis frontend
```

检查后端健康：

```bash
curl -f http://localhost:8080/health
```

如果返回成功，说明后端 HTTP 服务已可访问。

## 7. 访问地址

默认端口如下：

| 服务 | 地址 | 说明 |
| --- | --- | --- |
| Web UI | `http://localhost` | 前端入口，默认宿主机端口 `80`。 |
| 后端 API | `http://localhost:8080` | REST API。 |
| 后端健康检查 | `http://localhost:8080/health` | app healthcheck。 |
| Swagger | `http://localhost:8080/swagger/index.html` | 仅 `GIN_MODE != release` 时可用。 |
| Langfuse | `http://localhost:3000` | 可观测性控制台。 |
| MinIO Console | `http://localhost:9001` | 对象存储控制台。 |
| MinIO API | `http://localhost:9000` | S3 API。 |
| Neo4j Browser | `http://localhost:7474` | 图数据库控制台。 |
| Qdrant REST | `http://localhost:6333` | Qdrant REST API。 |
| SearXNG | `http://127.0.0.1:8888` | 默认只绑定 loopback。 |
| MCP Server | `http://localhost:8082` | WeKnora MCP Server 对外端口。 |

如果端口冲突，可在 `.env` 中改对应端口，例如：

```bash
FRONTEND_PORT=8088
APP_PORT=18080
LANGFUSE_WEB_PORT=13000
MINIO_PORT=19000
MINIO_CONSOLE_PORT=19001
SEARXNG_PORT=18888
MCP_PORT=18082
```

修改后重新启动：

```bash
docker compose --profile full up -d
```

## 8. 首次登录与初始化

### 8.1 注册首个用户

访问：

```text
http://localhost
```

如果 `DISABLE_REGISTRATION=false`，可以在登录页注册用户。注册成功后会创建默认工作区，并让用户成为该工作区 Owner。

如果你想关闭公开注册：

```bash
DISABLE_REGISTRATION=true
```

修改后重启 app 和 frontend：

```bash
docker compose --profile full up -d app frontend
```

### 8.2 设置系统管理员

系统管理员用于管理平台级设置，不等同于租户 Owner。

推荐流程：

1. 先在 Web UI 注册用户，例如 `admin@example.com`。
2. 在 `.env` 中设置：

```bash
WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL=admin@example.com
```

3. 重启 app：

```bash
docker compose --profile full up -d app
```

启动时，如果当前没有系统管理员，后端会把该用户提升为 System Admin。该操作是幂等的；已经存在系统管理员后，不会重复提升其他人。

### 8.3 配置模型

进入“设置 -> 模型”，至少配置：

- LLM / KnowledgeQA
- Embedding

建议再配置：

- Rerank：提升检索结果质量。
- VLM：处理图片、扫描 PDF、含图文档。
- ASR：处理音频。

添加模型后，使用“测试连接”确认可用。

### 8.4 创建知识库

1. 进入知识库页面。
2. 新建知识库。
3. 选择 LLM、Embedding、Rerank 等模型。
4. 设置分块策略，默认 `auto` 即可。
5. 如果需要 Wiki 或图谱，打开相应索引策略。

### 8.5 上传文档并验证

建议先上传小型 Markdown 或 TXT 文件，便于快速验证：

```markdown
# WeKnora Test

WeKnora 是一个用于 RAG、Agent 和 Wiki 的知识库框架。
```

上传后等待状态变成完成。然后在对话页面提问：

```text
WeKnora 是什么？
```

如果返回答案并带有引用来源，说明基本链路已经打通。

## 9. 启用和验证可选能力

### 9.1 Langfuse

如果使用自建 Langfuse：

1. 打开 `http://localhost:3000`。
2. 注册或使用自动初始化账号登录。
3. 进入 Project Settings 生成 API Key。
4. 回填 `.env`：

```bash
LANGFUSE_HOST=http://langfuse-web:3000
LANGFUSE_PUBLIC_KEY=pk-lf-...
LANGFUSE_SECRET_KEY=sk-lf-...
```

5. 重启 app：

```bash
docker compose --profile full up -d app
```

6. 发起一次知识库问答，等待数秒后在 Langfuse Traces 中查看 trace。

查看日志：

```bash
docker compose logs app | grep Langfuse
```

### 9.2 Neo4j 知识图谱

确认 `.env`：

```bash
ENABLE_GRAPH_RAG=true
NEO4J_ENABLE=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=password
```

启动或重启：

```bash
docker compose --profile full up -d neo4j app
```

在知识库设置中启用实体/关系抽取，上传文档后访问：

```text
http://localhost:7474
```

执行：

```cypher
MATCH (n) RETURN n LIMIT 50;
```

如果有节点和关系，说明图谱写入成功。

### 9.3 MinIO

如果 `STORAGE_TYPE=minio`：

1. 打开 `http://localhost:9001`。
2. 使用 `.env` 中的 `MINIO_ACCESS_KEY_ID` 和 `MINIO_SECRET_ACCESS_KEY` 登录。
3. 检查 bucket 是否存在。
4. 上传含图片文档后确认图片可以在 Web UI 正常展示。

本地默认账号通常是：

```text
minioadmin / minioadmin
```

生产环境必须修改。

### 9.4 SearXNG 网页搜索

`full` profile 会启动 SearXNG。默认宿主机访问：

```text
http://127.0.0.1:8888
```

在 WeKnora Web UI 的网页搜索设置中新增 provider：

```text
Provider: SearXNG
Instance URL: http://searxng:8080
```

容器内部访问必须使用 `http://searxng:8080`，不要用 `localhost:8888`。

### 9.5 Qdrant

如果使用 UI 创建向量库：

```text
Host: qdrant
gRPC Port: 6334
REST Port: 6333
Collection: weknora_embeddings
TLS: false
```

如果直接通过 `.env` 指定默认检索驱动：

```bash
RETRIEVE_DRIVER=qdrant
QDRANT_HOST=qdrant
QDRANT_PORT=6334
QDRANT_COLLECTION=weknora_embeddings
```

修改后重启 app：

```bash
docker compose --profile full up -d app
```

### 9.6 MCP Server

1. 在 Web UI 中进入“设置 -> API Keys”生成 API Key。
2. 回填 `.env`：

```bash
WEKNORA_API_KEY=sk-...
MCP_PORT=8082
```

3. 重启 MCP：

```bash
docker compose --profile full up -d mcp
docker compose logs -f mcp
```

MCP 容器内部访问后端的地址已经配置为：

```text
http://app:8080/api/v1
```

### 9.7 Dex / OIDC

`full` profile 会启动 Dex，并挂载 `misc/dex-config.yaml`。要在 WeKnora 中启用 OIDC，需要在 `.env` 中配置：

```bash
OIDC_AUTH_ENABLE=true
OIDC_AUTH_ISSUER_URL=http://127.0.0.1:5556/dex
OIDC_AUTH_DISCOVERY_URL=http://127.0.0.1:5556/dex/.well-known/openid-configuration
OIDC_AUTH_CLIENT_ID=client_id_for_oidc_client
OIDC_AUTH_CLIENT_SECRET=secret_for_oidc_client
OIDC_AUTH_SCOPES="openid profile email"
```

实际端点和 client 配置需与 `misc/dex-config.yaml` 对齐。

### 9.8 Agent Skills Sandbox

`sandbox` 服务不是常驻服务，它用于准备 `wechatopenai/weknora-sandbox` 镜像。app 执行 Skills 时会根据配置使用 Docker、local 或 disabled 模式：

```bash
WEKNORA_SANDBOX_MODE=docker
WEKNORA_SANDBOX_TIMEOUT=60
WEKNORA_SANDBOX_DOCKER_IMAGE=wechatopenai/weknora-sandbox:${WEKNORA_VERSION:-latest}
```

容器化部署中，如果 app 容器内不可用 Docker 命令，后端会尝试回退到 local；回退失败则禁用。需要严格 Docker 沙箱时，需根据部署环境额外提供 Docker CLI/Socket 或改为受控的外部执行方案。

## 10. 常用运维命令

### 10.1 查看日志

```bash
docker compose logs -f app
docker compose logs -f docreader
docker compose logs -f frontend
docker compose logs -f postgres redis
docker compose logs -f langfuse-web langfuse-worker langfuse-clickhouse
```

查看最近 200 行：

```bash
docker compose logs --tail=200 app
```

### 10.2 重启服务

```bash
docker compose --profile full restart app
docker compose --profile full restart frontend
docker compose --profile full restart docreader
```

按配置重新创建：

```bash
docker compose --profile full up -d app
```

### 10.3 停止服务

```bash
docker compose --profile full down
```

这会停止并删除容器，但保留 named volumes，数据不会被删除。

### 10.4 清空所有本地数据

危险操作，会删除数据库、文件、MinIO、Neo4j、Qdrant、Langfuse 数据：

```bash
docker compose --profile full down -v
```

执行前请确认已经备份。

### 10.5 更新镜像

```bash
docker compose --profile full pull
docker compose --profile full up -d
```

升级后 app 默认会自动执行数据库迁移。

### 10.6 检查迁移状态

如果怀疑迁移失败，先看 app 日志：

```bash
docker compose logs app | grep -i migration
```

手动迁移脚本在：

```bash
./scripts/migrate.sh
```

常用命令：

```bash
./scripts/migrate.sh version
./scripts/migrate.sh up
./scripts/migrate.sh force <version>
```

`force` 会改变迁移版本标记，只有确认数据库实际状态后再使用。

## 12. 附录：额外可选组件

### 12.1 Milvus

Milvus 不在 `full` profile 中。启动：

```bash
docker compose --profile milvus up -d
```

配置：

```bash
RETRIEVE_DRIVER=milvus
MILVUS_ADDRESS=milvus:19530
MILVUS_COLLECTION=weknora_embeddings
MILVUS_METRIC_TYPE=IP
```

### 12.2 Weaviate

Weaviate 不在 `full` profile 中。启动：

```bash
docker compose --profile weaviate up -d
```

配置：

```bash
RETRIEVE_DRIVER=weaviate
WEAVIATE_HOST=weaviate:8080
WEAVIATE_GRPC_ADDRESS=weaviate:50051
WEAVIATE_SCHEME=http
```

### 12.3 Apache Doris

Doris 不在 `full` profile 中。启动：

```bash
docker compose --profile doris up -d
```

配置：

```bash
RETRIEVE_DRIVER=doris
DORIS_ADDR=doris-fe:9030
DORIS_HTTP_PORT=8030
DORIS_DATABASE=weknora
DORIS_USERNAME=root
DORIS_PASSWORD=
DORIS_TABLE_PREFIX=weknora_embeddings
```

### 12.4 OpenDataLoader hybrid

`odl-hybrid` 是本地构建镜像，不发布到 Docker Hub，也不属于 `full`：

```bash
docker compose --profile odl-hybrid up -d --build odl-hybrid
```

相关配置：

```bash
DOCREADER_ODL_HYBRID=docling-fast
DOCREADER_ODL_HYBRID_URL=http://odl-hybrid:5002
ODL_HYBRID_EXTRA_ARGS=--no-ocr
```

仅在确实需要 OpenDataLoader hybrid 解析能力时再启用。
