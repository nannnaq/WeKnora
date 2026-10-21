# 192.168.18.152 部署记录

部署日期：2026-10-12。源码直接位于 `/mnt/storage/cjy/WeKnora`，来源为
`https://github.com/nannnaq/WeKnora.git`，基准提交 `2c014cde`。
项目级 Git 代理为本机已有的 `http://127.0.0.1:7897`。

访问入口：**http://192.168.18.152:9750/**。使用 103 上原有的账号和密码登录。
后端健康检查：`http://192.168.18.152:9751/health`。

## 服务与端口

应用、前端和 MCP 从本仓库源码构建为 ARM64 镜像。本机系统为 openEuler
22.03 LTS-SP2 / aarch64。以下数据库均是 WeKnora 独立实例，使用独立数据目录、
账号和口令，不依赖本机其他项目的公共数据库。

| 服务 | 本机端口 | 说明 |
| --- | --- | --- |
| 前端 | 9750 | Nginx 代理本机后端 |
| 后端 | 9751 | 容器内部 8080 |
| SearXNG | 9752 | 使用本机已有 7897 网络代理 |
| MinIO API / 控制台 | 9753 / 9754 | 独立 MinIO，桶为 weknora |
| Neo4j HTTP / Bolt | 9755 / 9756 | 独立图数据库，已启用 APOC |
| Qdrant REST / gRPC | 9757 / 9758 | 独立向量数据库 |
| MCP | 9770 | `/mcp`，需要鉴权 |
| PostgreSQL | 容器内部 5432 | 独立 PostgreSQL 17，未占用宿主机现有 5432 |
| Redis | 容器内部 6379 | 独立 Redis 7，未发布到宿主机 |
| Docreader | 容器内部 50051 | 与 103 相同的 v0.7.1 ARM64 镜像 |

所有对外发布端口绑定 `192.168.18.152`，保留 103 的端口号。
PostgreSQL、Redis、Docreader 使用本项目 Docker 网络中的服务名通信，
与 103 一样保留内部端口，不与其他项目抢占宿主机端口。
实时上下文接口为本机 `9707`（实体图谱）和 `9701`（规则库）。
Langfuse 保持 103 的应用配置 `LANGFUSE_ENABLED=false`。

## 迁移范围与校验

103 全程只读；通过在线 SQL 导出、S3 GET、Qdrant scroll、只读 Cypher、Redis
DUMP/PTTL 读取数据，未在 103 创建快照、停止服务或修改配置。

| 数据 | 迁移数量 / 校验 |
| --- | --- |
| PostgreSQL 项目业务表 | 53 张，逐表记录数一致 |
| 知识库 | 2 个，包含内部聊天历史知识库；页面显示业务知识库 test |
| 文档记录 | 32 条 |
| 分块 | 2177 条 |
| 用户 | 1 个，原密码哈希保留 |
| MinIO 文档及解析产物 | 402 个对象，191735778 字节，逐个 SHA-256 校验 |
| Qdrant | 原 1024 维集合 6090 条；重建 2560 维集合 6090 条 |
| Neo4j | 5212 个节点、908 条关系，按 WeKnora 知识库标签和文档 ID 限定 |
| Redis | 导出 73 个键，迁入 65 个任务历史/统计键；排除 8 个源工作进程心跳键 |

原 `SYSTEM_AES_KEY` 已保留，用于解密迁移的 API Key 和其他业务凭据。
本机数据库和对象存储使用新口令，MCP 使用迁移的项目 API Key。
迁移过程中临时加入公共 PostgreSQL/MinIO 的项目数据库、账号及桶已清理，
未保留公共数据库依赖。

## 模型配置

`config/builtin_models.yaml` 是实际挂载的内置模型配置：

| 用途 | 地址 | 模型 |
| --- | --- | --- |
| 对话 / 视觉 | `http://192.168.18.152:7860/v1` | `qwen3-8-27b` |
| Embedding | `http://192.168.18.152:8001/v1` | `/workspace/model_embedding_w8a8`，2560 维 |
| Rerank | `http://192.168.18.152:8002/v1` | `/workspace/model_reranker_w8a8` |

对话和视觉模型显式关闭思考：`chat_template_kwargs.enable_thinking=false`。
本机补充了聊天模型对 `extra_config.enable_thinking: "false"` 的支持，
使后台请求、流式请求和显式开启思考的调用也遵守该模型设置；相关单元测试已通过。
重排阈值已针对本机模型调整为 0.1。原 1024 维向量保留备份，实际检索使用 2560 维集合，
并保留源集合的 multilingual 全文分词设置。

已验证：页面和健康接口、原项目 API Key、文档下载、向量检索、基于迁移文档的问答及引用、
无思考输出、用户提供的图片识别、MCP 初始化、网页搜索，以及 systemd 服务重启。

## 自启动与运维

系统单元为 `/etc/systemd/system/weknora.service`，已启用；项目容器设置
`restart: unless-stopped`。单元等待 Docker、网络和数据盘挂载完成。

```bash
cd /mnt/storage/cjy/WeKnora
sudo systemctl status weknora.service
sudo systemctl restart weknora.service
docker compose --env-file .env -f deploy/local152/compose.yaml ps
docker compose --env-file .env -f deploy/local152/compose.yaml logs -f app
```

不要使用原始根目录的 `docker-compose.yml` 管理本机部署，应使用上面的
`deploy/local152/compose.yaml`，以免启动其他配置的数据库实例。

## 数据与备份位置

- 源码、构建依赖、迁移备份及运行数据均在 `/mnt/storage/cjy/WeKnora`。
- 运行数据：`.weknora-deploy/runtime/` 下的 postgres、redis、minio、qdrant、neo4j、files 等目录。
- 只读迁移备份和校验清单：`.weknora-deploy/migration/`。
- 构建和验收日志：`.weknora-deploy/logs/`。
- Docker 镜像和构建缓存：已有 Docker 数据目录 `/mnt/storage/docker_dir`。
- 口令和密钥：`.env`，权限 600；迁移备份目录权限 700，均不纳入 Git。

独立 PostgreSQL 使用普通 PostgreSQL 17 存储项目元数据；103 ParadeDB 的附加
GIS/搜索扩展系统表不作为项目数据复制。实际检索与 103 一样由 Qdrant 提供。
