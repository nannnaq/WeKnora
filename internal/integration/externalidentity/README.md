# 外部账号接入

可选的服务端账号桥，供 DSH Hub 等可信平台建立成员自己的 WeKnora 账号。默认关闭，不改变普通注册、邮箱登录、OIDC 或普通邀请码加入时的权限。

## 配置与部署

将 [配置模板](../../../config/external_identity.example.json) 复制到私有目录。`WEKNORA_EXTERNAL_IDENTITY_CONFIG_FILE` 指向该 JSON；配置文件及每个 `token_file` 均须为 `0600` 普通文件。接入密钥至少 32 字符，由平台与 WeKnora 后端各自私有保存，不交给浏览器。必须保留已有 `SYSTEM_AES_KEY`，用于账号恢复证明和租户凭证的加密保存。

每个来源配置 `id`、不可变平台身份 `issuer`、密钥文件、共享空间邀请码和 `viewer` 或 `editor` 角色。来源可以有多个，禁止通过此接入模块授予空间管理员。邀请码须有效且目标空间无需审批；变更目标空间或 issuer 需要明确迁移已有绑定。密码和角色不会从普通用户页面传入。

152 部署可使用 [可选 Compose overlay](../../../deploy/local152/compose.external-identity.yaml)：将配置保存为 `.weknora-deploy/external-identity/config.json`，接入密钥保存为同目录 `provisioner.key`。`issuer` 填 Hub `/platform` 的 `platformId`，邀请码填实际私有值。该目录不纳入 Git；容器中的运行用户须能读取文件。

启用时在仓库根目录使用两份 Compose 文件，并先构建包含本模块的新后端镜像：

```bash
docker compose --env-file .env \
  -f deploy/local152/compose.yaml \
  -f deploy/local152/compose.external-identity.yaml config --quiet
```

使用 overlay 启用的实例，后续启动和更新均使用相同文件。152 的实际部署已将这些挂载和环境显式持久化到基础 Compose 文件，因此原 systemd 单元无需追加 overlay；当前镜像、备份与恢复见 [152 部署记录](../../../deploy/local152/README.md#2026-10-28-dsh-外部账号桥部署)。模块本身默认关闭，其他未配置的部署不会自动启用集成。

## 接口

`POST /api/v1/integrations/external-accounts/:source/ensure` 和 `POST .../:source/revoke` 使用该来源专用的 `Authorization: Bearer ...`；拒绝带 Cookie 或 Origin 的请求。两者都接收 `issuer`、`subject`、正整数 `revision`，ensure 还接收 `username`。`subject` 是来源平台不可变成员 ID，用户名只用于展示和首次命名。

ensure 返回 `success: true` 和 `data`：`user_id`、`username`、`tenant_id`、`api_key_id`、`api_key`、`organization_id`、`role`。revoke 返回 `success: true`。响应禁止缓存；错误只返回稳定代码，不透传服务、数据库或凭证内容。

Hub 每次连接与断开的意图切换递增 revision。相同意图、相同 revision 可以重试，较旧请求或同版本的相反意图返回 409，防止延迟开通恢复已撤销的 Key。接口只供受信平台后端使用，不能作为网页 SSO 或任意账号认领入口。

## 数据与恢复

- 身份键为来源 ID、issuer 与 subject。用户名冲突时添加稳定后缀，内部邮箱按身份生成；不合并其他同名账号，也不放宽原邮箱校验。
- 创建原生个人工作空间及 Owner 成员关系。账号不具有系统管理员或跨工作空间特权；共享空间按配置授予 editor/viewer，具体共享知识库仍受分享权限限制。
- 专用 Key 仅作用于该个人工作空间，使用租户级完整访问以支持个人知识库管理及共享编辑；不签发平台级 Key。后续插件仍需明确开放自己的工具。
- 自有表 `integration_external_identity_bindings` 在扩展启用时独立 AutoMigrate，不占用官方顺序迁移编号。保留账号、工作空间、加密恢复证明和分步进度；仅凭邮箱相同无法认领账号。
- 撤销只处理该绑定的集成 Key，保留账号、共享成员关系及知识库资料。重新连接复用身份并取得新 Key。重试会清理同一绑定的过期有效 Key；不得借此清理用户手动创建的其他 Key。
- PostgreSQL 使用绑定行锁协调多进程。SQLite Lite 使用进程内锁和逐步持久化，运行一个后端进程。备份包含扩展表、原生账号/工作空间/Key 表及原 `SYSTEM_AES_KEY`。

## 验证

```bash
go test ./internal/integration/externalidentity
```

测试使用独立临时数据库和原生注册、Owner、API Key、共享权限服务，覆盖成员隔离、编辑权限、管理拒绝、同名冲突、失败重试、并发、重启恢复、撤销与旧请求拒绝、专用认证、关闭模块及配置权限。实际部署、真实账号数据与长期容量需要在对应环境另行核验。

多服务并发可额外设置 `WEKNORA_IDENTITY_TEST_POSTGRES` 为一次性 PostgreSQL DSN 并运行 `go test -race ./internal/integration/externalidentity`。该测试只接受名为 `weknora_identity_test` 的数据库，创建独立随机 schema 并在结束时清理，验证两个服务对象处理同一身份时只保留一个账号和有效 Key。跨仓联调由 DSH 的 `npm run accept:weknora` 启动本包 `TestHubFixture`，使用临时 SQLite；普通测试不启动常驻 HTTP 服务。
