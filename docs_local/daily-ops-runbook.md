# WeKnora 日常启动、重启与排查速查

适用范围：当前目录的 Docker Compose 全量部署，默认使用 `--profile full`，端口从 `9750` 开始。

## 1. 日常启动

```bash
cd /mnt/local4t/cjy/WeKnora
docker compose --profile full up -d
docker compose --profile full ps
```

启动后常用访问地址：

| 服务 | 地址 |
|---|---|
| Web UI | `http://服务器IP:9750/` |
| 后端健康检查 | `http://服务器IP:9751/health` |
| MinIO 控制台 | `http://服务器IP:9754/` |
| Neo4j | `http://服务器IP:9755/` |
| Qdrant REST | `http://服务器IP:9757/` |
| Langfuse | `http://服务器IP:9769/` |
| MCP | `http://服务器IP:9770/mcp` |

快速健康检查：

```bash
curl -f http://localhost:9751/health
docker compose --profile full ps
docker compose logs --tail=100 app
```

## 2. 停止、重启、更新

只重启业务后端：

```bash
docker compose --profile full restart app
```

重启前端：

```bash
docker compose --profile full restart frontend
```

停止整套服务但保留数据卷：

```bash
docker compose --profile full down
```

拉取最新镜像并重启：

```bash
docker compose --profile full pull
docker compose --profile full up -d
```

不要随便执行：

```bash
docker compose down -v
```

`-v` 会删除数据库、MinIO、Qdrant、Neo4j、Langfuse 等数据卷，只有确认要清空环境时才用。

## 3. 修改代码后怎么重启

当前生产 Compose 默认跑镜像。改了源码后，需要重新 build 对应服务。

| 修改内容 | 推荐命令 |
|---|---|
| 后端 Go 代码、迁移、`internal/`、`cmd/` | `docker compose --profile full up -d --build app` |
| 前端 `frontend/` | `docker compose --profile full up -d --build frontend` |
| 文档解析 `docreader/` | `docker compose --profile full up -d --build docreader` |
| MCP `mcp-server/` | `docker compose --profile full up -d --build mcp` |
| `docker-compose.yml` 端口、镜像、环境变量 | `docker compose --profile full config --quiet && docker compose --profile full up -d` |
| `.env` | 按影响服务重启；不确定时执行 `docker compose --profile full up -d` |
| `config/builtin_models.yaml` | `docker compose --profile full restart app` |
| `misc/dex-config.yaml` | `docker compose --profile full restart dex` |

本地开发模式可用热更新，不必每次构建镜像：

```bash
make dev-start DEV_ARGS=--full
make dev-app
make dev-frontend
```

## 4. 常用维护命令

查看状态：

```bash
docker compose --profile full ps
docker stats
```

查看日志：

```bash
docker compose logs -f app
docker compose logs --tail=200 app frontend docreader postgres redis qdrant minio neo4j
docker logs --tail=200 WeKnora-mcp
docker logs --tail=200 dex
```

检查 Compose 配置是否能解析：

```bash
docker compose --profile full config --quiet
```

备份 PostgreSQL：

```bash
mkdir -p backups
docker exec WeKnora-postgres pg_dump -U postgres WeKnora > backups/weknora-$(date +%F).sql
```

清理无用镜像：

```bash
docker image prune
```

## 5. 常见故障排查

先看三件事：

```bash
docker compose --profile full ps
docker compose logs --tail=200 app
docker compose logs --tail=200 postgres minio neo4j qdrant
```

常见问题：

| 现象 | 优先检查 |
|---|---|
| `WeKnora-app` unhealthy | 看 `docker compose logs app`，再检查 Postgres、Redis、MinIO、Qdrant、Neo4j 是否启动 |
| MinIO 退出 | `.env` 中 `MINIO_ACCESS_KEY_ID` 至少 3 位，`MINIO_SECRET_ACCESS_KEY` 至少 8 位 |
| Neo4j 退出 | `NEO4J_PASSWORD` 不能是默认值 `neo4j` |
| `langfuse-db-init` 首次失败 | 多数是 Postgres 未完全 ready；等 Postgres healthy 后再执行 `docker compose --profile full up -d` |
| MCP 一直重启 | 检查 `.env` 中 `MCP_SERVER_AUTH_TOKEN` 是否为空 |
| MCP 能启动但工具调用失败 | 检查 `.env` 中 `WEKNORA_API_KEY` 是否已填入有效租户 API Key |
| 模型调用失败 | 检查 `.env` 的 Qwen / DeepSeek Key 和 `config/builtin_models.yaml`，修改后重启 `app` |
| 端口冲突 | 修改 `.env` 里的宿主机端口，保持容器内部端口不变 |
| 前端打不开但 app 正常 | 看 `frontend` 日志，确认 `FRONTEND_PORT=9750`、`APP_HOST=app`、`APP_BACKEND_PORT=8080` |

## 6. 日常注意事项

- `.env` 是本地敏感配置，不要提交到 Git。
- 数据主要在 Docker volume 中；重装、清理、迁移前先备份。
- 修改 `SYSTEM_AES_KEY`、`CRYPTO_MASTER_KEY`、`CRYPTO_SALT` 这类加密密钥前要确认影响，改错可能导致旧凭据无法解密。
- 当前端口规划从 `9750` 起，新增暴露端口继续递增，避免占用服务器已有端口。
