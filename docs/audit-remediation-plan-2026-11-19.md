# 全链路审计与修复规划（2026-11-19）

> 审计方式：五个并行只读子代理分片排查（隧道生命周期 / 通知邮件 / 认证安全接线 / WireGuard+worker / 前端消费链路）+ 人工逐条核实关键发现。
> 本文分两部分：**一、本轮已直接实施的小修清单**（已全部通过构建/测试/交叉编译）；**二、待审核的大改项**（设计取舍或需要测试矩阵支撑，审核通过后按优先级实施）。
>
> **进度更新**：P1-A rebuild 重构已于 2026-11-19 经用户确认后实施完成（保留式 upsert，行 ID/规则字段不再漂移），回归测试 `services/dao/rebuild_proxy_config_test.go` 四用例覆盖（行 ID 保留 / 停止行保留 / 幂等 / 失败回滚）；下文 P1-A 标记为已完成，实施顺序顺延。

---

## 一、本轮已直接实施的小修（21 项）

| # | 级别 | 位置 | 问题 → 修复 |
|---|---|---|---|
| 1 | P0 | biz/master/client/update_tunnel.go:226 | goroutine 内 origin 查询失败时对 nil 解引用（无 recover，panic 击穿 master）→ 空值守卫 + 用已知字段打日志 |
| 2 | P0 | services/master/grpc_server.go:240 | **上线提醒在连接断开时触发**（`<-done` 之后才发，语义颠倒：掉线瞬间收到"已上线"、真重连反而无通知）→ 移入注册成功分支 |
| 3 | P1 | services/rpc/client.go | CallClient **无超时**（节点假死时 HTTP 请求悬挂、对账卡死、chan/map 双泄漏）→ 30s 超时 + 缓冲 channel + 统一 Delete（不再 close，消除迟到投递 panic 面） |
| 4 | P1 | services/rpc/client.go:84 | Recv 忙转：connector 被 Remove 后 `Get==nil` 无 sleep 死循环（CPU 100% + 日志洪泛 + 掉线漏报）→ 启动时捕获 connector、Get 失败即退出并发 done |
| 5 | P1 | services/rpc/client.go:84 | 重连后旧 Recv goroutine 每轮重新 Get 拿到**新** connector，双 goroutine 并发 Recv 同一流（gRPC 禁止）→ 同上修复（connector 绑定） |
| 6 | P1 | services/rpc/client.go:69 | EVENT_ERROR 分支泄漏 chan+map 条目 → defer 统一 Delete |
| 7 | P1 | services/mailer/mailer.go:150 | SMTP 会话拨号后无 deadline：半开连接时 goroutine/socket 永久挂死（HTTP 同步测试邮件也随之悬挂）→ 会话级 SetDeadline |
| 8 | P1 | biz/master/proxy/update_proxy_config.go | **编辑隧道即清空**定时/限额规则、限额方向、手动暂停（Stopped）、WorkerID（全新实体 Save 全量覆写列）→ 从旧行继承上述字段（与规则对话框的整体替换语义不冲突：规则由 update_proxy_rules 独立管理） |
| 9 | P1 | biz/master/proxy/create_proxy_config.go:144 | 创建隧道时 UpdateFrpcHander 错误被吞 → 跨服务器场景会"删旧未建新"；且 UI 误报成功 → err 正确返回 |
| 10 | P1 | biz/master/wg/wireguard_create.go | WireGuard 创建**不校验 client 归属**：知道其他租户 clientID 即可向受害者节点注入自己控制私钥的 WG 接口（跨租户注入，与已修 shell P0 同类）→ 补 client 归属校验 |
| 11 | P1 | biz/master/wg/network_delete.go | DeleteNetwork 只删网络行：残留 WG 在 30s 内被各节点 PullWireGuards 触发 nil 解引用 panic **击穿 master**（gRPC 无 recovery）→ 级联删 WG + 尽力下发删除事件 |
| 12 | P1 | services/master/grpc_server.go | gRPC 无 panic recovery：任一 handler panic 即进程崩溃 → 增加 recovery 拦截器 |
| 13 | P2 | biz/master/wg/client_list_wireguards.go + get_network_typology.go | `networkPeers[id][0].Network.ACL.Data` 对悬挂数据裸解引用（#12 的直接 panic 源）→ 空值/预加载缺失防御 |
| 14 | P2 | models/wireguard.go:151 | ToPB 把 WsListenPort 写成 ListenPort → ws 传输端口全部错配 → 一行修复 |
| 15 | P2 | services/mailer/notify.go:414 | 手动停用隧道/客户端会触发假"隧道已上线"通知（补发只查 disappeared 未查 activeNames）→ 补发必须 activeNames 真实命中 |
| 16 | P2 | services/dao/proxy.go | 删除客户端后 `Stopped=true` 的隧道以幽灵行残留（删除路径复用了 rebuild 的 stopped 过滤）→ `DeleteProxyConfigsByClientIDOrOriginClientID` 增加 `includeStopped` 参数：删除场景 true、rebuild 场景 false |
| 17 | P2 | services/workerd/exec_manager.go | RunCmd check-then-store 非原子 → 并发双启动产生不可取消的孤儿 run loop，uid 永久无法再启动 → LoadOrStore |
| 18 | P2 | services/watcher/client.go | gocron 任务无 SingletonMode：对账任务超 60s 周期时两轮并发重复启停隧道/重复发通知 → 加 SingletonMode |
| 19 | P2 | biz/master/server/sync_client_configs.go | 下游同步用节点 owner 构造 userInfo 调 UpdateClient（WHERE 带归属过滤）：多用户共享节点时其他用户的 child 更新 0 行且无报错 → 按 child 自己的归属构造 |
| 20 | P3 | services/master/grpc_server.go:222 | 节点注册 secret 普通比较（dao 层已常量时间，此处遗漏）→ subtle.ConstantTimeCompare |
| 21 | P3 | biz/master/server/sync_client_configs.go | 多用户前缀剥序边界、每 30s 重复查 clients 两个（部分已在昨日接入时处理）→ 记录待办见二-13 |

## 二、待审核的大改项（按优先级排列）

### P1-A 重建机制重构：`RebuildProxyConfigFromClient`（结构性问题，最优先）✅ 已实施（2026-11-19）

**现状**：每次隧道 create/update/start/stop 都会触发"全删全插"式重建：
1. 按 `OriginClientID = child.ClientID` 查旧行——而 proxy_config 行的 OriginClientID 是**父 ID**，child 上永远查不到 → 全部走 INSERT；
2. 新实体只填连接段，`db.Save` 全量覆写；
3. 先 `DeleteProxyConfigsByClientIDOrOriginClientID(false)` 删非停止行。

**后果**：隧道 A 配置的定时/限额规则在编辑/启动任意隧道 B 后被静默清空（auto rules 整体失效）；proxy_config 行 ID 每次漂移，按行 ID 关联的 resource_notify_prefs 全部变孤儿。

**方案**：rebuild 改为**保留式 upsert**——按 `(ClientID, Name)` 匹配旧行：命中则保留 Model/规则/Stopped/AutoStopped 字段、只更新连接段 Content；未命中才 INSERT；已不存在的名字才 DELETE（显式列出）。同步处理 resource_notify_prefs 的行 ID 引用（改为按 clientID+name 定位或级联迁移）。

**影响面**：update_proxy_config.go/start_proxy/stop_proxy/create_proxy_config 的规则继承补丁可简化；traffic 详情、通知偏好恢复稳定。
**工作量**：中。**风险**：rebuild 是隧道写入的唯一汇聚点，需要以内存库 + MySQL 双后端回归全部隧道操作路径（create/edit/start/stop/跨 server 迁移）。

### P1-B 节点 RPC 层单属主重构 ✅ 已实施（2026-11-20）

在途请求表从全局 `ClientRecvMap` 迁移到连接私有的 `defs.Connector.Pending`（RegisterPending/DeliverPending/FailAllPending），流结束（EOF/错误/被移除）时 FailAllPending 立即失败该连接全部在途请求（等待方收到明确错误，不再依赖 30s 超时兜底）；Recv goroutine 启动时绑定 connector、Get 失败即退出（消除忙转与双 Recv 并发）；CallClient 保留 30s 超时兜底（缓冲 channel 使迟到投递不 panic 不泄漏）；`GetClientRecvMap/SetClientRecvMap` 全局 API 已删除。
剩余增强（暂缓）：应用层 ping 探活检测半开连接（EVENT_PING 30s 已提供基本保活）。

### P1-C 删除级联统一入口 ✅ 已实施（2026-11-20）

现状各删除路径只覆盖一半（本轮已修 delete_client 的幽灵行），仍有：
- `DeleteServer` 不级联：该节点全部 proxy_config/child clients/stats/tick 残留；
- `DeleteClient` 不清理 WG（endpoints 悬挂、节点上 wg 接口永不删除）、不发 EVENT_REMOVE_WORKER；
- `RemoveFrpcHandler`（/frpc/delete）不清理 proxy_config 与通知偏好；
- wireguard_links 双向边删除依赖反向边存在。

**方案**：新增 dao 级 `CascadeDeleteClient/CascadeDeleteServer`（隧道、统计、tick、hourly、通知偏好、WG、worker 关联一次性处理 + 对节点尽力下发清理事件），四个删除入口全部换用。**工作量**：中。**风险**：删除语义变化，需逐路径列清单测试。

### P1-D 禁用物理外键迁移 ✅ 已实施（2026-11-20，路线 B）

原方案（默认 DSN 加 foreign_keys pragma）在实施中发现**物理外键路线不可靠**：gorm v1.25.11 对 "references 指向非主键唯一列" 的 belongs-to 声明（WireGuard.Client/Endpoint.Client references:ClientID），把物理 FK 约束建到了**父表** clients 上（DDL 实测：clients 表出现指向 wireguards/endpoints 的反向 CASCADE 约束，方向完全错乱，插入父表即失败）。

**改走路线 B**：`conf.DbGormConfig()` 统一设置 `DisableForeignKeyConstraintWhenMigrating: true`——AutoMigrate 不再创建任何物理外键约束（新库/重建表均无），级联删除语义统一由服务端显式级联清理承担（P1-C，待实施），彻底消除 "sqlite/MySQL 行为不一致" 与 "存量表无 FK" 的双重问题。
模型上今天临时添加的 6 处 constraint 标签已回滚（避免打开 FK 时踩反向约束）；回归测试 models/foreign_keys_test.go 守护"迁移后无物理外键"。

### P2-E 掉线检测周期对账兜底 ✅ 已实施（2026-11-20）

掉线/防抖状态全在内存：master 重启后，重启前已掉线且未重连的节点**永远不会产生掉线通知**（漏报窗口=重启周期）。
**方案**：新增周期任务对比「DB 应在线（clients/servers 未停止）vs ClientsManager 实际在线」，超过配置防抖时长的补发通知（与 CheckNodeOffline 共用去重）。**工作量**：中。

### P2-F 通知可靠性增强 ✅ 已实施（2026-11-20，第 1/2/3/4 项；SendAsync 队列与 server 删除联动为 P1-C 承担）

1. `TrackProxyOffline`：`notified` 在发送前置位，SMTP 抖动期间的掉线通知静默永久丢失 → 改为发送失败回滚标记（下一轮重发）；
2. `TrackProxyOffline` 持全局锁期间做 DB 查询（prefs 解析）→ 移到锁外；
3. `SendAsync` 无并发上限/重试 → 信号量限并发；
4. proxyPending/proxyNotified 在节点删除后无清理 → server 删除钩子联动清理（并入 P1-C）；
5. `firstSync || len(activeNames)==0` 时整表 reset 会误清已通知隧道的上线配对状态 → 拆分语义。

**工作量**：小～中。

### P2-G 安全卫生批（小改集合）✅ 大部分已实施（2026-11-20）

已实施：FRPAuth 按用户限流 + frp token 日志脱敏（rpc_frp_auth.go）、`?token=` 访问日志打码（middleware/logger.go GinAccessLogFormatter）、SetTrustedProxies(nil)、logout 改 POST + header/query token 吊销（logout.go revokeJWT 三源）、节点注册 secret 常量时间比较、SMTP 密码长度上限 95 字节、SMTP 设置保存互斥（smtpSaveMu）、secret 钩子解密失败保留原密文（不再静默置空）。
未实施（需前端配合/中改）：gRPC Pull*/FRPCAuth 之外的失败限流细化、改密要求旧密码+会话吊销（需 pb/前端）、logout GET 别名移除需确认前端已切 POST。

1. wsgrpc 方法级失败无限流：gRPC 层加按 IP 失败计数（/auth 已有限流，等价面未覆盖）；`rpc_frp_auth.go:34` 用户 frp token 比较改常量时间；
2. `rpc_frp_auth.go:15` 的 `req [%+v]` 日志含用户 frp token 明文 → 脱敏；
3. `?token=` JWT 进 gin 默认访问日志 → log formatter 对 `token=` 打码；
4. logout 改 POST（GET 可被跨站 `<a>` 强制登出 DoS），并同时吊销 header/query token 的 jti；
5. 改密：要求旧密码 + 触发该用户全部会话吊销（sign_token 签发的 24h header token 目前泄露后无法吊销）；
6. gin 加 `SetTrustedProxies(nil)`：登录限流按 `c.ClientIP()`（XFF 最左值）可被伪造绕过；可叠加按账号维度限流；
7. SMTP 密码长度校验（明文 >95 字节时密文超 varchar(255)，MySQL 严格模式报错/非严格静默截断）；
8. `UpdateSMTPSettings` 读-改-写无锁，并发保存丢更新（密码保留逻辑交错回滚）→ 改单条 UPDATE 或加互斥；
9. `secret_hooks.go` AfterFind 解密失败静默置空 → 读-改-写路径会把空串回写**永久抹掉凭证**：解密失败时保留原密文并 Error 日志，避免无声数据损毁（GLOBAL_SECRET 不匹配场景）。

### P2-H workerd 链路 ✅ 已实施（2026-11-20）

已实施：run loop 指数退避（3s→60s 封顶，成功重置）；Pdeathsig=SIGKILL（防孤儿进程占用抽象 socket）；自动发现 /usr/local/bin 优先、剔除 cwd（防劫持修复不再被架空，conf 不再引用 cwd）；ConfigTemplate 改 text/template + 解析失败 log+skip（不再 panic）；UpdateWorker clientIds 为空时保持原节点集合（消除全网停机 ~30s）；InstallWorkerd 提取后清理下载临时目录 + DownloadFile 失败路径清理；pin 失败信息附升级路径（删 pin 重装重新 TOFU）；ExtractGZTo 跨文件系统 EXDEV 回退拷贝。
未实施（记录）：workerd 状态死字段（GetWorkerStatus 走 ProcessExistsBySelf 已可用）、Windows no-op 明确禁用、per-user worker 配额、RedeployWorker 重试（PullWorkers 30s 兜底已覆盖）。

1. 无限重启循环无退避/上限/失败上报（每 3s 重启刷错误日志）→ 指数退避 + 上限 + 状态上报；
2. TOFU pin 无版本维度：workerd 永久无法升级（换版本哈希必不匹配）→ pin 记录版本→哈希映射或显式 reset；
3. frpp 崩溃后 workerd 孤儿进程（无 Pdeathsig）→ SysProcAttr.Pdeathsig + 启动清理旧实例；
4. 自动发现顺序 cwd 优先于 /usr/local/bin，架空防劫持修复 → PATH 优先、剔除 cwd；
5. UpdateWorker 只改代码时对全部节点先 remove 后 create（依赖 30s pull 兜底，全网停机 ~30s）→ clientIds 为空时保持原集合只发 update；
6. ConfigTemplate 用 html/template 生成非 HTML 配置（转义破坏 capnp 内容）且解析失败 panic（每 30s 刷 panic）→ text/template + 入库校验；
7. RedeployWorker remove 成功 create 失败无重试（worker 消失）→ 失败重试/回滚；
8. 下载临时目录失败路径不清理、跨文件系统 EXDEV 无 copy 回退。

### P2-I WireGuard 剩余 ✅ 部分已实施（2026-11-20）

已实施：
1. `UpdateWireGuard` 移除 endpoints 硬删 Clear，`Omit(clause.Associations)`（端点绑定由 handler bind/unbind 管理，不再摧毁 links）；
2. Update 补 LocalAddress 校验（AllocateIP 同口径，占用自动顺延，防全网 pull 失败）；
3. `CreateWireGuard` Omit 关联（消除双重创建撞唯一索引）；
4. ws 传输 HTTP server 纳入生命周期（wireGuard 结构体加 wsHTTPServer 字段，Stop 关闭——重启不再端口占用/泄漏）；
5. gvisor MTU 配置化 + DNS 解析失败项跳过（不再写零值地址）；
6. StopAllServices 首错不中断；CreateService 覆盖前停旧实例（防实例泄漏）。

剩余（中改，待后续）：PatchPeers 与 Add/RemovePeer 并发丢更新；gvisor 路由变更重放；(network_id, local_address) 唯一索引消 TOCTOU；types_wg key getter panic 改 error；UAPI IPv6 裸地址；per-user WG 配额与 pageSize 上限；Link 双向边/查重。

---

原始清单：
8. defs/types_wg.go key getter panic 改返回 error；endpoint IPv6 裸地址 UAPI 非法；per-user 配额与 pageSize 上限。

### P2-J 前端 ✅ 已实施（2026-11-20）

已实施：client_stats_card 加 `enabled: !!clientID` 守卫 + 刷新 setTimeout 卸载清理；queryKey 补参（clientsStatus/clientStatus 加 clientType ×6 处、selector 三处 key 加 pageSize、proxyTrafficRange 加 clientID/serverID）；新增 `www/lib/safe-parse.ts`（parseJSONSafe）替换代理列表/actions/worker ingress/frpc 卡片/visit-preview 的行级裸 JSON.parse（单条脏配置不再打挂整页）；traffic-ranking-table 补 isError 分支；i18n 补齐 wg.networkDetail.updatedAt/noTime、worker.info.description/description_placeholder/unknown_version（四语种）。
未实施（记录）：zh-tw/fr 22 个历史欠账 key（fallbackLng='zh' 兜底，可批量补）；header/sidebar 登录态竞态（需统一 store）、代理页行级轮询优化（批量接口）、BigInt 展示缩放、$proxyTableRefetchTrigger 全局单通道拆分——均为 P3 体验项。

1. `client_stats_card` 缺 `enabled: !!clientID` 守卫（进页面即发空查询 + 持续轮询）；
2. queryKey 补参：`clientsStatus`/`clientStatus` 加 clientType（4 文件）、`proxyTrafficRange` 加 clientID+serverID 且 find 比对、client/agent/server-selector 的 key 加 pageSize；
3. 行级裸 `JSON.parse` 无保护（单条脏配置打挂整页，_app 无 ErrorBoundary）→ safeParse 兜底 + frpc_editor 提交前 JSON 校验 + 顶层 ErrorBoundary；
4. 错误状态呈现：排行表/状态列/统计卡请求失败被伪装成"暂无数据" → 补 isError 分支；
5. header/sidebar 登录态双查询竞态（已登录用户偶发被闪跳 /login）；
6. 代理页行级轮询叠加（~2 req/s）→ 批量状态接口或降频（中改）；
7. i18n：zh-tw/fr 补 22 个可达 key；4 个四语种全缺 key（wg.networkDetail.updatedAt 等）；home.trend.title_hour 潜伏可达；
8. BigInt 展示：平台级"本年"合计接近 9PB 时 Number 精度丢失 → bigint 域缩放后再转换；
9. `www/i18n/index.ts` 死代码与 lib/i18n.ts 双实例、语言恢复链路断裂（FOUC）。

### P2-K 会话失效自动跳登录（2026-11-24，用户实测反馈）

问题：JWT 过期后页面只显示"请求失败"，不跳登录页；长时间挂在 SPA 里永不检测。

根因（三处叠加）：
1. `common.ErrUnAuthorized` 的 JSON 分支回 code **500**（`Err()`），与"服务内部错误"无法区分——前端拿不到任何会话信号；
2. `api/http.ts` 无响应错误拦截器：401 只被当成普通 `throw msg`，不清 token、不跳转；
3. 登录态只由 sidebar 里**一次性**的 `['userInfo']` 查询判定（无 `retry`/无轮询），`$token` 是持久化的、过期后仍在，`$statusOnline` 一旦为 true 永不复核；Header 的跳转 effect 因此永远不触发。

已实施：
- 后端 JSON 分支改回 `UnAuth(err)`（code 401，HTTP 200 不变，protobuf 分支仍回 HTTP 401）；回归测试 `common/response_test.go` 守护；
- 前端新增 `www/lib/session.ts`：`clearAuthState()` + `handleSessionExpired(reason)`（清 `$token`/`$userInfo`/`$statusOnline` → `window.location.replace('/login?redirect=...')`，登录/注册页不跳、防重入）；
- `api/http.ts` 加响应拦截器：body `code===401` 或 HTTP 401 → 统一走 `handleSessionExpired`；请求拦截器修掉无 token 时发 `Bearer undefined`；
- 新增 `www/components/session-guard.tsx` 挂在 `Providers`（覆盖 terminal 等不走 RootLayout 的页面）：本地无 token 直接跳；有 token 则 60s 心跳校验（覆盖 jti 被吊销/用户被删/管理员封禁等"未过期但已失效"）；
- Header 的 `['userInfo', pathname]` 查询加 `refetchInterval: 60_000`（原来只查一次），跳转改 `replace` 并带 `redirect` 回跳参数；`RegisterAndLogin` 删掉重复的 `['userInfo']` 查询（两个 key 各判一次的竞态来源）；
- 登录页消费 `?redirect=`（只接受站内相对路径，防开放重定向）后 `router.replace` 回原页面；
- 顺手修 `middleware/logger.go` 零延迟分支少一个 `%s` 参数的格式 bug（go vet 报错、日志输出 `%!s(MISSING)`）。

未做（记录）：`/api/v1/auth/*` 失败限流按 IP 之外的维度细化；token 失效时前端 toast 提示"登录已过期"（当前直接跳转）。

### P3 已知且暂缓（此前会话遗留，维持暂缓）

多节点时区不一致的展示层对齐（stats_date 已保证数值不丢不重）、多租户下 get_by_sid 统计可见性（admin 视图）、`frp RenderWithTemplate` 环境渲染等——详见 docs/security-audit-2026-11-15.md 与本文 P1-A/J 关联项。

---

## 实施顺序建议

1. **P1-A rebuild 重构**（结构性根因，附带删除二-8 中已并入的临时补丁说明）
2. **P1-B RPC 单属主** + **P1-D foreign_keys**（同一回归矩阵一起测）
3. **P1-C 级联统一**（依赖 D 的测试）
4. P2-E/F/G/H/I/J 按 P2 顺序逐批

每批完成后跑：`go build ./... && go test ./...` + `frpp check all` + 手动回归（创建/编辑/启停/删除隧道 × 多客户端多服务端 × 通知开关）。
