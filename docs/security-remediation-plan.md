# 安全修复实施规划（基于 2026-11-15 安全审计）

> 整理日期：2026-11-15 ｜ 状态：**四阶段全部实施完成（2026-11-15）**；推迟项（wsgrpc ticket、改密验证旧密码）见验收清单
> 依据：[security-audit-2026-11-15.md](/security-audit-2026-11-15.md)（3 P0 / 14 P1 / 12 P2 / 11 P3）
> 原则：每项修复附带单测或可复现的验证步骤；破坏性/兼容性变更在条目中显式标注；默认只破坏攻击面、不破坏合法功能；改动不提交，留工作区审阅
>
> **确认方式**：回复"按推荐方案实施"即按本文档执行；或指出调整项。每阶段完成后停下来汇报再进下一阶段。

## 决策点速览（不指定时按推荐方案执行）

| # | 决策点 | 推荐方案 | 备选与影响 |
|---|---|---|---|
| A | JWT 密钥派生 | **保留 SHA1 公式但启动拒绝默认/弱密钥**（零破坏；`frpp check` 已有检测项，启动 FailHard 化） | 改为独立随机密钥文件：一次性作废全部会话与已签发 token |
| B | OTA/workerd 下载 URL | **保留租户可触发，但 URL 锁定官方 release 模式 + 强制 SHA-256 校验**（既修 RCE 又保留自助升级体验） | 字段收归管理员：体验受损，最安全 |
| C | frp 配置模板 | **租户配置中拒绝一切 `&#123;&#123;` 模板标记**（frp 模板在面板场景几乎无合法用途；`&#123;&#123;.Envs.*&#125;&#125;` 即为泄露面） | 仅黑名单 `&#123;&#123;.Envs`：仍存在 `&#123;&#123;if&#125;&#125;` 类模板注入面 |
| D | TLS/传输默认值 | **收紧为安全默认**（`TLS_INSECURE_SKIP_VERIFY=false`、`COOKIE_SECURE=true`），配 TOFU 式 CA 引导与迁移文档 | 维持现状仅文档警告：MITM 面保留 |
| E | RBAC/casbin 死代码 | **删除 casbin 依赖，改为服务端角色→路径白名单的简单推导**（admin 全放行，普通用户走资源归属校验） | 真正启用 casbin：策略体系成本高，当前无必要 |
| F | workerd 运行身份 | **unix 下降权运行（专用 uid/gid，SysProcAttr），windows 保持现状** | 维持 root：保留 P2 风险 |
| G | ghfast.top 代理 | **默认关闭 + 忽略租户提交的代理参数**（签名校验落地后风险已消大半，此项为纵深） | 保留：用户依赖镜像加速时自行显式开启 |

---

## 一、修复批次总览

| 阶段 | 主题 | 条目 | 工作量 | 破坏性 |
|---|---|---|---|---|
| 阶段 1 | P0：跨租户 RCE 三连 + 路径穿越 | 6 | 1~1.5 天 | 无 |
| 阶段 2 | P1 核心：鉴权、模板、下载链路、租户补齐 | 9 | 2~3 天 | 少量（明确标注） |
| 阶段 3 | 传输与存储收紧、会话、RBAC（P2 主体） | 10 | 2~3 天 | 有（决策点 D/E/F/G） |
| 阶段 4 | P3 卫生 + 安全基线固化为 checker | 8+ | 1 天 | 无 |

统一验证：`go build ./...`、`go test ./...`（受影响包）、交叉编译四平台、临时库端到端复现"修复前可利用 / 修复后被拒绝"。

---

## 二、阶段明细

### 阶段 1：P0 跨租户 RCE（最高优先，全部非破坏性）

#### 1.1 资源类路由统一归属校验
新建 `services/dao/ownership.go` 或在现有 DAO 上明确封装：`AssertClientOwnership(userInfo, clientID)`（owner 或 `role==admin` 通过；内部走 `GetClientByClientID`）。接线四处：
- [biz/master/shell/handler.go](../biz/master/shell/handler.go) PTY（P0-1）
- [biz/master/streamlog/get_log.go](../biz/master/streamlog/get_log.go) 日志流（同时删 `ACAO: *`）
- [biz/master/platform/get_clients_status.go](../biz/master/platform/get_clients_status.go) 逐 ID 校验（FRPC/FRPS 两种类型分别校验归属）
- worker 附加链路见 1.3
**验收**：租户 A 请求租户 B 的 clientID → 403；owner 与 admin 正常。

#### 1.2 workerId/CodeEntry 白名单 + 写入 containment
- [biz/master/worker/create_worker.go](../biz/master/worker/create_worker.go)、update_worker.go：校验 `WorkerId`/`CodeEntry` 命中 `^[A-Za-z0-9_-]+$`（错误信息明确）
- [services/workerd/file.go](../services/workerd/file.go) 写路径统一经过 `safeWorkerRelPath()`（复用/修正 SafeWorkerID 语义：仅剥字符不够，改为白名单校验 + `filepath.Rel(workerdCWD, resolved)` 不以 `..` 开头的 containment 断言）
- 顺带修复 [services/workerd/workerd.go:83-88](../services/workerd/workerd.go) RemoveAll 前缀检查（P2-3，同文件顺手）
**验收**：构造 `CodeEntry=../../evil` 与 `WorkerId=../../..` 的创建/写入全部被拒；正常 ID 功能不变；单测覆盖穿越与正常两类。

#### 1.3 worker 附加链路租户过滤
- [services/dao/client.go](../services/dao/client.go) `GetClientsByClientIDs` 补 `user_id = ? AND tenant_id = ?`（admin 角色放行全量）
- [biz/master/worker/update_worker.go](../biz/master/worker/update_worker.go)、redeploy_worker.go：传入真实 userInfo；同时删除这两处把含 ConnectSecret 的完整 client 行写入 Info 日志的代码（P2-6 子项）
**验收**：单测——租户 A 试图附加租户 B 的 clientID → 返回空/报错；日志中不再出现 ConnectSecret。

#### 1.4 删除反向 AuthAdmin 死代码
[middleware/auth.go:49-53](../middleware/auth.go)（dead admin-assign 分支）与 :62-70（反向 AuthAdmin）：删除；阶段 3 新的 admin 门禁按决策点 E 重新实现，避免误用。
**验收**：编译通过；grep 确认无引用残留。

#### 1.5 JWT nil-exp panic 修复（P3 顺手，同文件 1 行）
[middleware/jwt.go:100-102](../middleware/jwt.go)：`GetExpirationTime()` 判空后再解引用。

#### 1.6 修复后行为固化
新增 checker 单测或检查项：`auth/pty_ownership`（destructive 门控下验证跨租户 PTY 被拒）——放入阶段 4 统一做，本阶段先保证修复本身有单测。

---

### 阶段 2：P1 核心（鉴权 / 模板 / 下载链路 / 租户补齐）

#### 2.1 JWT 启动拒绝默认/弱密钥（决策点 A）
[cmd/frpp/shared/providers.go](../cmd/frpp/shared/providers.go) 或 conf.Complete()：`GLOBAL_SECRET == "frp-panel"` 或长度 < 16 → `Panicf` 阻止启动（提示设置强密钥）。docker 镜像文档同步。
**验收**：默认配置启动被拒；设置强密钥正常；checker 的 global_secret 检查项结论一致。

#### 2.2 登录/注册限流与统一错误响应
新增 `middleware/ratelimit.go`（freecache 实现 per-IP + per-account 计数，窗口 15min / 上限 20 次，超限 429）；接线 `/auth/login`、`/auth/register`、`/auth/cert`、frps `/auth` 插件 handler。同时：
- 登录未知用户返回与错误密码一致的统一错误 + 虚拟 bcrypt 比较（消除枚举/时序）
- [biz/master/auth/handler.go:78](../biz/master/auth/handler.go) 换 `subtle.ConstantTimeCompare`
- 密码修改接口要求提供当前密码（update_user_info.go）
**验收**：单测限流窗口与放行；爆破 20 次后 429；统一错误文本；改密不带旧密码被拒。

#### 2.3 frp 配置模板标记拒绝（决策点 C）
[biz/master/client/update_tunnel.go](../biz/master/client/update_tunnel.go)、biz/master/server/update_tunnel.go：解析前扫描原始配置，包含 `&#123;&#123;` 即拒绝并提示"面板不支持配置模板（存在环境变量泄露面）"。
**验收**：提交含 `&#123;&#123;.Envs.APP_GLOBAL_SECRET&#125;&#125;` 的配置被拒且密钥不出现在任何存储/响应中；正常配置不受影响。

#### 2.4 OTA/workerd 下载链路加固（决策点 B）
- [biz/common/upgrade/download_url.go](../biz/common/upgrade/download_url.go)：URL 锁定官方 release 域名（GitHub releases / 显式配置的 OTA 源）+ 强制 https
- 下载后强制 SHA-256 校验（release manifest 或版本内嵌 checksum；上游无 manifest 时先实现"构建产物旁挂 sha256 清单"约定）
- [biz/common/upgrade/upgrade.go](../biz/common/upgrade/upgrade.go)：忽略租户提交的 `TargetPath/ServiceName/ServiceArgs`（改用节点本地 install.sh 的既有值），`HttpProxy` 忽略租户值
- [biz/master/worker/install_workerd.go](../biz/master/worker/install_workerd.go)：同样校验和 + 固定安装目录 + 解压大小上限（gzip bomb）
- ghfast 代理：默认关闭 + 忽略租户提交值（决策点 G）
**验收**：非官方 URL 被拒；篡改一字节的产物校验失败拒绝替换；正常官方升级全流程可走通。

#### 2.5 endpoints 租户列补齐（迁移 002）+ DAO 收敛
- [models/wireguard.go](../models/wireguard.go) EndpointEntity 增加 `TenantID int`/`UserID int`（带 index）→ AutoMigrate 加列
- **迁移 002** `backfill_endpoint_ownership`：按 `endpoints.client_id → clients` 回填归属（Go 循环回填，方言可移植；孤儿行置 0 = 仅 admin 可见）
- [services/dao/endpoint.go](../services/dao/endpoint.go) 全部方法补租户过滤；`ListEndpointsWithFilters` 的 `wireguardID` 分支先校验 wireguard 归属；`CreateEndpoint` 校验 `ClientId` 属于调用者
**验收**：迁移测试（sqlite + 方言 CI）覆盖回填与幂等；租户 A 读/改/删/列租户 B 的 endpoint 全部 403/404。

#### 2.6 clientsstatus 归属校验（P0-1 关联面收口）
[get_clients_status.go](../biz/master/platform/get_clients_status.go)：逐 ID 归属校验（1.1 的封装复用；FRPS 类型走 `GetServerByServerID`）。

#### 2.7 文件与目录卫生（小项批）
- [utils/files.go:97-103](../utils/files.go) 下载暂存目录 0700、ExtractGZTo 解压大小上限与临时目录清理
- [utils/file.go:23](../utils/file.go) 写文件权限 0600、目录 0700
**验收**：单测断言权限位与上限截断。

---

### 阶段 3：传输/存储/会话收紧（含兼容性变更，决策点 D/E/F/G）

#### 3.1 传输默认收紧（决策点 D）
- `TLS_INSECURE_SKIP_VERIFY` 默认 false；[services/rpc/master.go:98-102](../services/rpc/master.go) httpCli 移除硬编码 skip
- CA 引导改 TOFU（首次连接时记录 CA 指纹，后续校验）
- `COOKIE_SECURE` 默认 true（文档注明反代场景需正确透传）；authed 响应补 `Cache-Control: no-store`
- **兼容说明**：自签/反代部署需按迁移文档设置 CA；release notes 标注 breaking
**验收**：正常 CA 引导 + 节点连接在新默认下走通；MITM 场景（自签假 CA）被拒。

#### 3.2 /wsgrpc 与绑定面
- WS 升级要求一次性 ticket（`/auth/ws-ticket` 短时换取，升级时校验）或至少校验 Origin 回环白名单（决策：ticket 方案，兼容 `?token=` 既有前端用法的过渡开关）
- server API（:8999）默认绑定 `127.0.0.1`（插件宿主本就指向 127.0.0.1；文档提供放开开关）
**验收**：无 ticket 的 WS 升级被拒；带 ticket 正常；server API 不再全网卡监听。

#### 3.3 日志脱敏
删除/降级以下日志：JWT 全文（middleware/rbac.go、jwt.go:94）、用户实体 `%+v`（auth.go:41）、节点配置 `PrintStr`（providers.go:280）、clientSecret（providers.go:322）；统一改为 ID/前缀哈希。
**验收**：grep 全仓无 token/secret 全值入日志路径；手动冒烟确认。

#### 3.4 会话与服务端吊销
- JWT 增加 `jti`；登出与改密时把 jti 写入 freecache 黑名单（TTL=剩余有效期）；resign 不再延长有效期
- [biz/master/user/sign_token.go](../biz/master/user/sign_token.go)：expiresIn 上限（如 24h）、permissions 由服务端按角色生成（忽略请求值）
- [middleware/rbac.go:78](../middleware/rbac.go) `MustCompile` → 预编译缓存 + `regexp.Compile` 错误降级为字面量匹配
**验收**：登出后旧 token 立即失效；自签 token 无法越权/无限期；畸形 regex 不 panic。

#### 3.5 敏感字段静态加密（迁移 003）
`Client.ConnectSecret`、`SMTPSettings.Password`：以 `GLOBAL_SECRET` 派生的 AES-GCM 密钥加密存储（读时解密、比较常量时间）。**迁移 003** `encrypt_secrets_at_rest`：读取明文→加密回写（可重复执行）。ConnectSecret 不做哈希的原因：需还原下发到 frpc 配置。
**验收**：迁移后 DB 内为密文；节点连接与配置下发不受影响；修复后启动期泄露面消除。

#### 3.6 RBAC 决策落地（决策点 E）
删除 casbin 依赖与 services/rbac；新增服务端角色→能力推导（admin=全量，普通用户=资源归属范围内），RBAC 中间件仅做角色断言；`requireSMTPAdmin` 换用统一封装。
**验收**：普通用户访问 SMTP 设置/平台信息接口被拒；admin 正常；casbin 相关测试移除。

#### 3.7 workerd 降权（决策点 F）
[exec_manager.go](../services/workerd/exec_manager.go)：unix 下 `SysProcAttr.Credential` 专用 uid/gid（安装脚本创建 `frpp-worker` 用户）；windows 维持现状；启动失败降级警告。
**验收**：linux 上 workerd 进程属主为专用用户；功能回归。

#### 3.8 其余 P2 小项
注册首管理员原子化（事务内判数）；`CheckOrigin` 校验 Origin；平台信息接口加 admin 门禁；join 文档去掉 shell 历史建议（改 `--token-file`？小改）。

---

### 阶段 4：P3 卫生 + 安全基线固化为 checker

- P3 批量：CA 有效期缩短至 3 年 + 轮换说明、worker 文件权限 0600、枚举与信息泄露小项
- **新增安全 checker（`frpp check` 扩展，预计 72→78 项）**：
  - `auth/wsgrpc_auth`：升级面是否要求 ticket
  - `auth/token_in_url`：检测 ?token= 通道是否启用
  - `env/tls_verify`：TLS_INSECURE_SKIP_VERIFY 状态提示
  - `data/endpoint_tenant_scope`：endpoint 租户列与回填完整性
  - `data/secret_encryption`：迁移 003 是否完成
  - `auth/rate_limit`：限流配置生效性
- 文档：docs/deploy-master.md 增补"安全默认值与迁移说明"；release notes 草稿

---

## 三、验证与交付纪律

1. 每阶段统一验证：build / test / 四平台交叉编译 / 临时库端到端复现"修复前可利用 → 修复后被拒"
2. P0 修复必须带可复现的攻击用例单测（跨租户 403、路径穿越拒绝）
3. 破坏性变更（阶段 3）在 release notes 草稿中逐条列出迁移步骤
4. 阶段 2 的迁移 002 与阶段 3 的迁移 003 遵循已建立的迁移规范（可重复执行、方言可移植、`frpp migrate` 可离线执行）
5. git 不提交，改动留工作区审阅；每阶段完成后更新本文件状态与记忆

## 四、验收清单（实施完成后逐项勾选）

- [x] 阶段 1：三处跨租户 RCE 复现路径全部被拒（含单测）；worker 路径穿越被拒；AuthAdmin 死代码移除
- [x] 阶段 2：默认密钥拒启、登录限流 429、模板标记拒绝、OTA 签名校验、迁移 002 完成且方言 CI 绿
      - 实施说明：改密需旧密码一项延期（需要在 UpdateUserInfoRequest 中新增字段并重生成 pb，随下一批 pb 变更一起做）；workerd 安装校验采用 https + 节点本地配置源 + TOFU SHA-256 指纹（Cloudflare 官方 release 无我方清单）；OTA 的 SHA-256 清单由 release workflow 新增的 checksum 步骤产出（.github/workflows/tag.workflow.yml），此前的历史 release 无法作为升级目标（fail-closed，属预期）
- [x] 阶段 3：传输默认收紧（TLS 默认校验/COOKIE_SECURE 默认 true/Cache-Control no-store）、CheckOrigin 校验、server API 默认 127.0.0.1、日志脱敏、JWT jti+吊销黑名单、resign 不续期、sign_token 服务端权限+24h 上限、RBAC 正则缓存、casbin 移除、迁移 003 敏感字段加密（AES-256-GCM + GORM 钩子）、workerd 降权（frpp-worker）+ 目录移交、注册原子化、平台信息 admin 门禁
  - 实施说明：wsgrpc ticket 认证**推迟**（会破坏 ws-transport 节点与 `?token=` 前端用法，仅落地 Origin 校验 + server API 回环绑定 + JWT 吊销；后续配合前端改造再上 ticket）；修改密码需验证当前密码**推迟**（需要 pb 字段变更 + protoc 重新生成，列入后续）
  - ⚠️ 兼容性变更：`COOKIE_SECURE` 默认 true、`TLS_INSECURE_SKIP_VERIFY` 默认 false、弱 `GLOBAL_SECRET` 拒绝启动——自签/HTTP 反代部署需按 release notes 显式配置
- [x] 阶段 4：P3 卫生项（CA 3 年 + 轮换说明、worker 目录 0700、join `--token-file`）+ 6 个安全 checker 上线（72→78：auth/wsgrpc_auth、auth/token_in_url、env/tls_verify、data/endpoint_tenant_scope、data/secret_encryption、auth/rate_limit）+ 文档（deploy-master 安全默认值与 rekey 说明、checker.md 矩阵、release notes 草稿 docs/release-notes-security-2026-11.md）+ **附加：`frpp migrate rekey` 密钥轮换子命令**（用户要求，幂等 + 自动备份 + 6 个单测 + CLI 冒烟）
- [x] security-audit-2026-11-15.md 各条目标注修复状态
