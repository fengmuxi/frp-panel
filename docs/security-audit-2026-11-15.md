# 网络安全审计报告（mxfrp / frp-panel fork）

> 审计日期：2026-11-15 ｜ 审计人：安全维护视角 ｜ 方法：三线并行代码审计（认证与密钥、命令执行与供应链、租户隔离与暴露面），P0/P1 关键结论均经人工复核源码确认
> 范围：当前工作区全量 Go 代码 + 部署脚本（install.sh）+ Web 前端交互面
> 性质：防御性自查。本 fork 源自 VaalaCat/frp-panel，以下问题大概率同样存在于上游，可考虑 responsibly disclosure。

## 总体结论

**多租户授权模型存在系统性缺口**：多数"资源类"路由只做了"登录校验"而未做"资源归属校验"，其中 3 个可直接导致跨租户 RCE；节点侧二进制/代码分发链路完全信任租户参数且无完整性校验；JWT 密钥可由公开信息推导；全站无任何限流。SQL 注入专项核查为**干净**（全参数化）。

---

## P0 —— 立即修复（跨租户 RCE，已人工复核）

### 1. 任意登录用户可获得任意节点的交互式 Shell ✅ 已修复（2026-11-15，归属断言前置到 WS 升级前，含攻击复现单测）
- **位置**：[biz/master/shell/handler.go:44-72](../biz/master/shell/handler.go)，路由 `GET /api/v1/pty/:clientID`（handler.go:147）
- **证据**：`clientID := c.Param("clientID")` 后直接 `rpc.CallClient(ctx, clientID, EVENT_START_PTY_CONNECT, ...)`，链路无任何资源归属校验；节点侧 `utils/pty.Start()` 以 frpp 进程权限起 shell（install.sh 以 **root systemd 服务**运行 frpp）
- **影响**：任意租户用户（clientID 含用户名前缀、可枚举）→ 任意在线节点的 root shell，即跨租户 RCE
- **修复**：CallClient 前用 `GetClientByClientID(userInfo, clientID)` 校验归属；为 `services/rpc/client.go` 增加 owner 维度的统一校验

### 2. worker 代码写入路径穿越 → root 任意文件写入 ✅ 已修复（2026-11-15，API 白名单校验 + 写入前 containment 断言，含复现单测）
- **位置**：[services/workerd/file.go:18-37](../services/workerd/file.go)（`CodeFilePath`/`WorkerCWDPath`）
- **证据**：`filepath.Join(workerdCWD, WorkerInfoPath, worker.GetWorkerId(), "src", worker.GetCodeEntry())` —— `WorkerId` 与 `CodeEntry` 原样拼接；`SafeWorkerID` 仅用于 capfile 模板渲染，未用于文件路径；写入用 `os.MkdirAll`+`os.Create`；worker 创建校验（biz/master/worker/create_worker.go:64-74）只查非空
- **影响**：租户提交 `CodeEntry = ../../../../etc/systemd/system/x.service` 等即可在节点（root）上写入任意路径任意内容（持久化后门）；Join 的 lexical clean 不阻止 `..`
- **修复**：create/update 时校验 `WorkerId`/`CodeEntry` 命中 `^[A-Za-z0-9_-]+$`；写入前增加 containment 校验（`filepath.Rel(workerdCWD, resolved)` 不得以 `..` 开头）

### 3. worker→client 附加无租户过滤 → 向受害者节点部署代码 ✅ 已修复（2026-11-15，GetClientsByClientIDs 补租户过滤，admin 放行，含复现单测）
- **位置**：[services/dao/client.go:122-135](../services/dao/client.go)（`GetClientsByClientIDs`）+ [biz/master/worker/update_worker.go:36-98](../biz/master/worker/update_worker.go)
- **证据**：`db.Where("client_id IN ?", clientIDs)` —— 函数签名接收 `userInfo` 但完全未使用；UpdateWorker 将查到的 client 挂到自己的 worker 并下发 `EVENT_CREATE_WORKER`（携带 worker Code）
- **影响**：租户把受害者 clientID 挂进自己的 worker → master 将攻击者的 workerd JS 代码部署到受害者机器（每个受害租户一次 RCE）
- **修复**：`GetClientsByClientIDs` 补 `user_id = ? AND tenant_id = ?` 过滤（redeploy_worker.go 同）

---

## P1 —— 尽快修复（修复状态 2026-11-15：#4/5/6/7/10/11/12/14 已实施；#8/9 部分缓解——server API 默认绑 127.0.0.1 + CheckOrigin 校验 + JWT 吊销，wsgrpc ticket 认证推迟；#13 部分缓解——路径白名单 + 拒绝模板标记，端口/出口白名单待做）

| # | 问题 | 位置 | 要点与修复 |
|---|---|---|---|
| 4 | JWT 密钥可推导 + 默认密钥 | [conf/helper.go:27-29](../conf/helper.go)、settings.go:22 | `JWTSecret=SHA1(APIHost:APIPort:GLOBAL_SECRET)`，GLOBAL_SECRET 默认 `frp-panel` 且 APIHost 即公开域名 → 任何人可算出 HS256 密钥伪造 `x-vaala-userid:1` 管理员 token。**修复：启动时拒绝默认/短密钥；改独立随机密钥；弃 SHA1** |
| 5 | 登录/注册/cert/auth 插件零限流 | biz/master/auth/*（全仓无 limiter） | 无限密码爆破与用户名枚举（未知用户走不同响应路径）。**修复：per-IP+per-account 尝试限流 + 统一错误响应** |
| 6 | WireGuard endpoint 全 CRUD 无租户过滤 | [services/dao/endpoint.go:42-124](../services/dao/endpoint.go) | GetEndpointByID/Update/Delete/List 仅按 ID 或全表；`/wg/endpoint/list` 全表暴露；`?wireguard_id=` 无归属校验。**修复：补租户列或经父级归属校验** |
| 7 | 日志流 IDOR | [biz/master/streamlog/get_log.go:25-65](../biz/master/streamlog/get_log.go) | `?id=` 无归属校验，可流式读取任意节点 frpc 日志；响应带 `Access-Control-Allow-Origin: *` 放大泄露 |
| 8 | master API 默认全接口明文 HTTP | conf/helper.go:31-33、settings.go:27,35 | `:9000` 监听所有接口、`API_SCHEME=http`、`COOKIE_SECURE=false` —— 凭证/Cookie/配置明文。**修复：默认 https/wss 或无 TLS 拒绝启动** |
| 9 | `/wsgrpc` 升级无鉴权 + insecure 凭据 | cmd/frpp/shared/providers.go:161-163,198-202、master.go:44 | WS 升级无 token、gRPC insecure、CheckOrigin=true —— 密钥猜测的无限尝试通道（仅靠每 RPC 秘钥校验兜底） |
| 10 | 节点→master 链路 TLS 不验证 | settings.go:54、services/rpc/master.go:98-102、utils/cert.go:60-71 | `TLS_INSECURE_SKIP_VERIFY` 默认 true；`httpCli()` 硬编码 skip；**连 CA 引导（/api/v1/auth/cert）本身也走不验证通道** → MITM 可喂假 CA 并全程劫持。**修复：TOFU 校验 CA、默认 false、删硬编码** |
| 11 | OTA 升级：租户可控 URL/TargetPath/ServiceName，无完整性校验 | [biz/common/upgrade/upgrade.go:76-141](../biz/common/upgrade/upgrade.go)、biz/master/client/upgrade_frpp.go:64-77 | `/api/v1/client/upgrade` 把 `DownloadUrl/TargetPath/ServiceName/ServiceArgs` 原样下发；下载只验 ELF/PE 机器类型，**无 checksum/签名**；替换任意路径 + systemd 重启 → 租户对名下节点 root RCE（也可"降级"到旧版本）。**修复：签名/cosign 校验 + 字段收归管理员或删除** |
| 12 | workerd 安装：租户 URL → 下载 → chmod 755 → root 执行 | biz/master/worker/install_workerd.go:26、services/workerd/workers_manager.go:85-125 | 同 11；installDir 落在 frpp 工作目录。**修复：管理员门禁 + 校验和 + 固定安装路径 + 解压体积上限** |
| 13 | worker capnp 模板租户可控 → 沙箱旁路 | [services/workerd/workerd_conf_gen.go:33-46](../services/workerd/workerd_conf_gen.go)、defs/const.go:157 | workerd 实际运行租户提交的 `ConfigTemplate`：可绑任意端口（`tcp:*:9999`）、任意网络出口。**修复：模板改为服务端固定渲染，或白名单 socket 形态** |
| 14 | frp 配置模板渲染泄露 master 进程环境变量 | utils/load.go:10-15 + frp `RenderWithTemplate`（glbEnvs=os.Environ()） | 租户在 frpc/frps 配置任意字段写 `&#123;&#123;.Envs.APP_GLOBAL_SECRET&#125;&#125;`，master 渲染后结果存库并回传给租户 → **面板级凭证（JWT 密钥源/DB DSN）泄露**。**修复：渲染用净化后的环境集，或对租户输入不执行模板** |

## P2 —— 排期修复（12 项，摘要；修复标注 2026-11-15：#1/3/4/6/7/8部分/9/10/11/12 已实施）

1. workerd 以 root 运行、无降权/资源限制/重启循环无上限（services/workerd/exec_manager.go:70-82）→ **已实施**：unix 降权 frpp-worker（SysProcAttr.Credential）+ worker 目录 chown；cgroups/资源上限未做；默认关闭 `ENABLE_FUNCTIONS` 未做
2. frps multiuser 插件过滤结果被丢弃 + 租户可加任意 HTTPPlugin（SSRF 到内网/元数据）（biz/master/server/update_tunnel.go:55-58）→ 未处理
3. `os.RemoveAll` 前缀式 containment 检查形同虚设（services/workerd/workerd.go:83-88）→ **已实施**：ensureUnderCwd（filepath.Rel containment 断言）
4. RBAC/casbin 是死代码（无 Enforce 调用点）；登录给所有用户 `*:*` 权限 claim；sign-token 可自签任意 permissions/无限期；rbac.go:78 对用户可控字符串 `MustCompile` → panic/ReDoS → **已实施**：casbin/services/rbac 整体删除，角色断言中间件替代；sign-token 服务端按角色生成 permissions + expiresIn 24h 上限；regex 预编译缓存 + QuoteMeta 降级
5. 全站无限流（含 frps `/auth`、`FRPCAuth`、30s 拉取循环无退避）→ **部分实施**：登录/注册/cert 插件路径 per-IP 限流（固定窗口 + 锁定）；FRPCAuth 与拉取循环未做
6. 敏感信息入日志：JWT 全文（rbac.go:24,31、jwt.go:94）、用户实体含 bcrypt 口径（auth.go:41）、节点配置含 GlobalSecret（providers.go:280,322）、client 行含 ConnectSecret（worker redeploy/update:84-101）→ **已实施**：全部改为 ID/计数输出
7. ConnectSecret/SMTP 密码明文存储；ConnectSecret 用 `!=` 比较（dao/client.go:61）→ 存 SHA-256 + 常量时间比较 → **已实施（方案调整为 AES-256-GCM 静态加密，迁移 003）**：GORM 钩子加解密 + subtle.ConstantTimeCompare（ConnectSecret 需还原下发故不做哈希）
8. JWT 接受 URL query 传递（jwt.go:29，`?token=`）→ Referer/代理日志泄露；`x-set-authorization` 回显；无 `Cache-Control: no-store` → **部分实施**：authed 响应统一 `Cache-Control: no-store`；`?token=` 通道保留（ws-transport 兼容，列入阶段 4 checker 观测）
9. PTY/upgrader `CheckOrigin` 恒 true（shell/handler.go:234-243）→ **已实施**：Origin host == r.Host 校验（providers.go 与 shell handler 双处）
10. ghfast.top 代理默认开启且租户可改（settings.go:30、cmd.go:371-376、upgrade_frpp.go:68）→ **已实施**：节点本地强制 UseGithubProxy=false，租户提交字段忽略
11. 登出仅清 cookie + sliding renew 续期到完整 CookieAge（无 jti/吊销；sign-token 小有效期会被静默续成 1 天）→ **已实施**：JWT 增加 jti + freecache 吊销黑名单（登出即失效）；resign 保留原始 exp（不再续期）
12. 首用户管理员注册可竞态产生多个 admin（register.go:26-57）→ **已实施**：注册互斥锁原子化首管理员判定

## P3 —— 卫生项（简列）

nil-exp panic（jwt.go:100-102，**阶段 3 已加防护**）、登录用户名枚举（限流已缓解 + 未知用户 dummy bcrypt 同代价响应，**阶段 2/4 已修**）、改密无需旧密码（推迟，需 pb 字段变更）、join token 进 shell 历史（**阶段 4 已修：--token-file**）、frp 插件 token 非常量时间比较（auth/handler.go:78，**阶段 1 已修**）、自签 CA 有效期 10 年且私钥入库（**阶段 4 已修：3 年 + docs/deploy-master.md 轮换说明**）、下载暂存 0777（**阶段 1 已修 0700**）、ExtractGZTo 无解压上限（gzip bomb）+ 残留临时目录（**阶段 1 已修**）、平台信息泄露（get_platform_info，**阶段 3 已加 admin 门禁**）、AuthAdmin 中间件反向且零调用（auth.go:62-70，**阶段 1 已删除**）、worker 文件 0666 创建（**阶段 4 已修：目录 0700 + WriteFile 0600**）。

## 确认良好的部分

- **SQL 注入：0 可利用点**（全参数化；唯一动态拼接是整数桶间隔，内部常量）
- 密码哈希 bcrypt cost 14 + 常量时间比较；无 alg:none 暴露
- SMTP 密码不回显（HasPassword 布尔模式）；证书仅存 DB 无文件落盘；静态资源 embed FS；无 `c.File` 类穿越面
- client/server/proxy/wireguard/network/link 等核心 CRUD 租户过滤正确（本次专项逐方法核对）
- 隐式 CSRF 防护（protobuf content-type）+ SameSite=Lax（本会话已修）

## 修复路线图建议

1. **第一批（P0，约 1-2 天）**：三个所有权校验（PTY/日志流/worker 附加/clientsstatus 统一走 `GetClientByClientID(userInfo, id)`）+ workerId/CodeEntry 白名单与 containment 校验 + 删除 AuthAdmin 死代码
2. **第二批（P1 关键）**：启动拒绝默认 GLOBAL_SECRET + 登录限流 + frp 模板环境净化 + OTA/workerd 下载签名与字段收权 + endpoint DAO 补租户
3. **第三批（收敛）**：TLS 默认收紧 + 日志脱敏 + 存储加密 + RBAC 决策（删或真启用）+ workerd 降权
4. 每批完成后在 `frpp check` 中补对应检查项（如：启动密钥强度、endpoint 租户列存在性、下载 URL 白名单），把安全基线固化为巡检

## 与 checker 工具的关系

本轮 checker 已覆盖：默认 GlobalSecret 检测（env/global_secret）、Cookie 属性含 SameSite（auth 组）、敏感文件权限、节点版本偏差。审计新发现的项（限流缺失、模板注入面、URL token）可按上表第 4 步逐步固化为检查项。
