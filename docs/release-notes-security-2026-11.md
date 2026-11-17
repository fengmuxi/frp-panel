# Release Notes 草稿 —— 安全加固版（2026-11-15）

> 本草稿基于 docs/security-remediation-plan.md 四阶段实施汇总，发版时按实际版本号调整。

## ⚠️ 升级前必读（Breaking Changes）

1. **APP_GLOBAL_SECRET 必须是强随机值**（>=16 字符，不得为默认值 `frp-panel`）——弱密钥 master 直接拒绝启动。
   生成：`openssl rand -hex 32`
2. **`APP_COOKIE_SECURE` 默认 true**——纯 HTTP 部署（无 TLS 反代）如登录异常：上 HTTPS，或临时显式 `APP_COOKIE_SECURE=false`（`APP_` 前缀不可省略，裸写 `COOKIE_SECURE` 不会被读取）。
3. **CLIENT_TLS_INSECURE_SKIP_VERIFY 默认 false**——节点侧自签证书需要将 master CA 纳入信任，或显式回退 `true`。
4. **server API（8999 端口）默认仅监听 127.0.0.1**——本机 frps 认证插件不受影响；曾直接暴露 8999 的部署请自行评估。
5. **首次启动自动执行数据库迁移 002/003**——sqlite 自动备份（`.bak-migration-v3-*`）；回滚旧版本前先确认库内迁移版本不高于旧二进制（`frpp migrate verify`）。
6. **所有 web 登录会话失效**（JWT 派生链路变化）——重新登录即可。
7. **`ENABLE_FUNCTIONS` 未按计划默认关闭**——workerd 功能开关维持现状，请按需在配置中关闭。

## 新增

- **数据库版本化迁移**：`schema_migrations` 版本表 + 启动自动迁移 + `frpp migrate status/up/verify` 离线管理 + sqlite 迁移前自动快照（滚动保留 5 份）+ MySQL/PostgreSQL 方言 CI。
- **敏感字段静态加密**（迁移 003）：节点/服务端 `connect_secret` 与 SMTP 密码以 AES-256-GCM（GLOBAL_SECRET 派生）加密入库，库文件泄露不再等于凭证泄露。
- **`frpp migrate rekey --old-secret=<旧值>`**：更换 GLOBAL_SECRET 后透明轮换加密密钥（幂等、自动备份），节点无需重新接入。
- **登录限流**：`/auth`、`/api/v1/auth/login`、`register`、`cert` 插件路径 per-IP 固定窗口限流（15 分钟 20 次 + 锁定），未知用户名同代价响应（防枚举）。
- **JWT 会话安全**：token 携带 `jti`，登出即服务端吊销（freecache 黑名单）；续签不再延长有效期；`sign-token` 服务端按角色生成权限、有效期上限 24h。
- **OTA 完整性**：升级/worker 安装仅允许官方 release 域名 + 强制 HTTPS + SHA-256 校验和（`.sha256` 清单，fail-closed）；租户提交的下载地址/目标路径/服务参数一律忽略。
- **worker 安全**：代码写入路径白名单 + containment 断言（防穿越）；unix 上 workerd 以专用 `frpp-worker` 用户降权运行（安装脚本需创建该用户，不存在则回退并告警）；worker 目录 0700。
- **`frpp check` 扩展至 78 项**：新增 `env/tls_verify`、`auth/wsgrpc_auth`、`auth/token_in_url`、`auth/rate_limit`、`data/endpoint_tenant_scope`、`data/secret_encryption` 六个安全基线检查。

## 修复（安全审计 2026-11-15）

- **P0×3**：跨租户 PTY shell、worker 路径穿越（root 任意文件写入）、worker 附加无租户过滤——全部修复并带攻击复现单测。
- **P1**：endpoint 全 CRUD 补租户过滤（迁移 002 回填归属列）、日志流 IDOR + `ACAO:*` 移除、OTA/workerd 下载校验、frp 配置模板拒绝 `&#123;&#123;`（环境变量泄露向量）、节点本地升级参数收权。
- **P2**：WS `CheckOrigin` 校验、日志全量脱敏（JWT/secret/用户实体不再入日志）、secret 常量时间比较、`MustCompile` panic/ReDoS 消除、casbin 死代码移除、注册首管理员原子化、平台信息接口 admin 门禁。
- **P3**：自签 CA 有效期 10 年→3 年、`Cache-Control: no-store`、nil-exp 防护、`?token=` 通道保留但提供 `--token-file`（避免进 shell 历史）。

## 已知推迟项

- `/wsgrpc` 一次性 ticket 认证：会破坏 ws-transport 节点与前端 `?token=` 用法，待前端改造后另版推出（当前以 Origin 校验 + 回环绑定 + JWT 吊销缓解）。
- 修改密码需验证当前密码：需 pb 字段变更。
- workerd cgroups/资源上限、frps multiuser 插件过滤结果丢弃（SSRF 面）：下批处理。

## 升级步骤（sqlite 部署示例）

```bash
# 1. 确认 .env 中 APP_GLOBAL_SECRET 为强随机值（否则改之）
# 2. 停止旧版 master
# 3. 备份数据目录（或依赖自动迁移快照）
# 4. 用新版启动 master（自动迁移 002/003）
# 5. frpp check --group auth --group env --group data 确认安全基线
```

换 GLOBAL_SECRET 的场景先看 deploy-master.md「换 GLOBAL_SECRET（敏感字段密钥轮换）」——必须走 `frpp migrate rekey`。
