# 自动化排查工具（frpp check）

`frpp check` 是 frp-panel 内置的全功能自动化排查命令：以**旁路进程**对 Master / Server / Client 部署执行 78 项只读健康检查，输出终端彩色表格、JSON 或 Markdown 报告，并可通过 `--fail-on` 直接接入 CI 门禁。

```
+-------------+   REST(protobuf/json)   +--------+      +---------+
| frpp check  | ----------------------> | Master | ---> |  DB     |
| (旁路进程)   |   gRPC(TLS/insecure)    |  frps  |      +---------+
|             |   ws/wss(/wsgrpc)       +--------+
|             |   wgctrl(仅 linux)      + 节点（server/client）
+-------------+
```

## 一、设计原则

1. **旁路探测**：check 不与 master 同进程，无法直接访问内存中的节点注册表（`rpc.GetClientsManager()`）与 freecache；这些状态通过 `/api/v1/platform/clientsstatus`（含 `STATUS_ONLINE/OFFLINE/ERROR` 与 `EVENT_PING` 毫秒延迟）与数据库（`clients.last_seen_at`）间接对账。
2. **默认只读**：所有检查默认无副作用。sqlite 以 `mode=ro` 打开（绝不会创建新库）；涉及写操作的用例（隧道 E2E、批量下发压测）必须显式 `--destructive`，且结束后立即清理。
3. **零硬编码**：端口、路径、URL、密钥派生全部来自 `conf.NewConfig()`（工作目录 `.env` → `/etc/frpp/.env` → 环境变量，与 master 一致）与 `defs` 常量；gRPC/WS 拨号复用 `utils/wsgrpc`，JWT 复用 `utils.ParseToken` 与 `conf.JWTSecret`。
4. **明确未覆盖项**：见第五节，不做虚假的"全绿"。

## 二、检查分组

| 分组 | 内容 | 代表检查项 |
|---|---|---|
| `env` | 环境与配置 | `.env` 存在性、`APP_GLOBAL_SECRET` 强度、**已废弃环境变量检测**（`MASTER_COMPATIBLE_MODE` 等，见下）、`DB_DSN` 连接与表结构、9000/9001/8999/profiler 端口监听、`CLIENT_RPC_URL/CLIENT_API_URL` 一致性、日志级别、OTA `status.json`、`MASTER_CACHE_SIZE`、节点身份、**磁盘与 WAL 水位**（`disk_watermark`，unix）、**敏感文件权限**（`file_permissions`，unix）、**TLS 校验开关**（`tls_verify`，`CLIENT_TLS_INSECURE_SKIP_VERIFY=true` 告警） |
| `auth` | 认证与安全 | 注册开关（`APP_ENABLE_REGISTER=false` 时拒绝）、错误口令拒绝、正确登录 + **Set-Cookie 属性断言**（Secure/HttpOnly/Path/Domain/Max-Age 与配置一致 + **SameSite 缺失/None 无 Secure 检测**）、伪造/过期 JWT 拒绝、11 个受保护路由组的匿名 401 矩阵、frps `/auth` multiuser 插件拒绝/放行、`/api/v1/auth/cert` 假凭证拒绝、**WS 升级面门禁**（`wsgrpc_auth`，/wsgrpc 拒绝非 WS 请求）、**?token= 通道观测**（`token_in_url`，日志泄露面提示）、**登录限流生效性**（`rate_limit`，连发失败登录应出现 429） |
| `master` | 控制面 | gRPC 一元探测（TLS/明文/ws/wss 四种传输，走项目 `wsgrpc` 真实代码路径）、`/wsgrpc` 路由存在性、节点在线状态 + `EVENT_PING` 延迟分布（min/avg/P99）、**节点版本偏差**（`node_version_skew`，对账 `ClientVersion.GitVersion`）、JSON 双协议兼容 |
| `server` | frps 生命周期 | DB 中全部 `frps.json` 解析（含高级模式）、bindPort/vhost/webServer/sshTunnelGateway 端口冲突矩阵（含与 master 端口重叠）、`/auth` 插件宿主可达性、流量统计新鲜度与单调性（无回退/溢出） |
| `client` | frpc 与隧道 | `frpc.json` 解析、proxy 类型覆盖矩阵（tcp/udp/http/https/stcp/sudp/xtcp/tcpmux，未知类型报错）、节点心跳新鲜度（`OfflineCheckDelay` 2 倍窗口）、本机身份与 DB 一致性、`natHoleSTUNServer` UDP 可达性、**grpc/ws/wss 三 transport 矩阵**、`--destructive` 隧道 E2E（创建+清理） |
| `worker` | workerd | workers 表与 socket JSON 完整性、workerd 二进制与 `--version`、磁盘代码 vs DB 代码 MD5 漂移、`proxy_config.worker_id` 引用完整性 |
| `wg` | WireGuard 组网 | network CIDR 合法性与 VPN 地址冲突、链路拓扑引用完整性（`wireguard_links`/`endpoints`）、**公网 relay 约束**（无公网 Endpoint 的网络给出明确提示）、`wgtypes.ParseKey` 密钥校验、listen_port/ws_listen_port 跨节点冲突、**设备级探测（仅 linux，build tag）**：接口/peer/最新握手/流量计数 |
| `webui` | 前端 | `GET /` 首页、`/_next/*` 静态资源抽测（**OTA 后产物错位检测**）、`/api/v1/log` 匿名 401、`www/lib/pb` 与 `idl/` 漂移、OTA 状态 vs 二进制构建时间 |
| `data` | 数据一致性 | 孤儿 proxy/worker/wg 记录、软删除审计、今日/累计流量对账、GORM 模型字段抽查（`schema_diff`）、schema 迁移版本对账（`db_migration`，比对 `schema_migrations` 与代码注册表）、**证书有效期**（`cert_expiry`，<7 天 fail/<30 天 warn）、**流量配额预警**（`quota_usage`，与 `proxy.CalcProxyQuotaUsed` 同口径）、**迁移备份新鲜度**（`backup_freshness`）、**endpoint 租户归属**（`endpoint_tenant_scope`，user/tenant=0 的行提示）、**敏感字段加密完成度**（`secret_encryption`，connect_secret/SMTP 密码应全部为 `enc:v1:` 密文） |
| `perf` | 性能基线 | `GET /` 压测 QPS/P50/P99（目标不可达时快速中止）、gRPC 一元 RTT 采样、`--destructive` 批量配置读取 |

### 关于"已废弃配置"的事实说明

以下变量**只存在于旧文档 `docs/all-configs.md`，当前 `conf/settings.go` 中已无对应字段**（文档自述"可能过期"）：

- `MASTER_COMPATIBLE_MODE` — 兼容官方 frp 客户端的能力由 PullConfig 时注入 `/auth` multiuser 插件（`biz/server/rpc_pull_config.go:InjectAuthPlugin`）实现，无需开关；
- `MASTER_INTERNAL_FRP_SERVER_PORT` / `MASTER_INTERNAL_FRP_AUTH_SERVER_HOST|PORT|PATH` — 内置 frps 地址由 `MASTER_INTERNAL_FRP_SERVER_HOST`（默认取 `MASTER_RPC_HOST`）决定，认证插件宿主固定为 `http://127.0.0.1:$SERVER_API_PORT/auth`。

`env/deprecated_vars` 会检测这些变量是否被设置并提示"不会生效"，避免按旧文档配置产生的"配置了却没效果"疑难杂症。同理，pprof profiler 实际监听 `0.0.0.0:DEBUG_PROFILER_PORT`（`cmd/frpp/shared/profiler.go`），`env/profiler` 在开启时给出安全警告。

## 三、编译

### 本地构建（build.sh）

```bash
# 完整构建（含前端），产物在 dist/
./build.sh --current

# 只编二进制（跳过前端，排查工具不依赖前端产物）
./build.sh --platform linux --bintype full --arch amd64 --skip-frontend
```

已验证的交叉编译矩阵：`linux/amd64`、`linux/arm64`、`darwin/arm64`、`windows/amd64`（`CGO_ENABLED=0`，sqlite 使用纯 Go 驱动 glebarez/sqlite）。WireGuard 设备探测文件带 `//go:build linux` 标签，非 linux 平台编译时自动降级为运行时 Skip。

### ko / Docker

```bash
# ko 构建镜像（与其他子命令共用同一二进制，无需改动 ko 配置）
ko build github.com/fengmuxi/frp-panel/cmd/frpp

# Docker 内运行（挂载配置与数据，共享宿主网络以便探测真实端口）
docker run --rm \
  -v /etc/frpp:/etc/frpp \
  -v /path/to/data:/data \
  --network host \
  ghcr.io/fengmuxi/frp-panel:latest check all --format json --out /data/report.json
```

## 四、使用

```bash
frpp check --list                          # 列出全部检查项与级别
frpp check all                             # 全量（等价于不带参数）
frpp check env auth master                 # 按组组合
frpp check wg --node node-a                # 定向节点（匹配 client_id/server_id/接口名）
frpp check data --only db_migration        # 只运行单个检查项（支持 group、group/name 或裸名）
frpp check all --skip perf                 # 跳过某组/某项
frpp check all --format json --out report.json
frpp check all --format markdown --out report.md
frpp check all --format prometheus --out /var/lib/node_exporter/textfile/frpp.prom   # 指标输出
frpp check all --fail-on major             # 存在 major 及以上 Fail 时退出码 1（默认值）
frpp check all --timeout 2m --checker-timeout 15s --concurrency 8
frpp check all \
  --login-user admin --login-password '***'   # 或环境变量 CHECKER_ADMIN_USER/CHECKER_ADMIN_PASSWORD
frpp check client --destructive            # 允许隧道 E2E（创建后自动清理）
frpp check all --notify-mail ops@example.com   # 失败/告警摘要邮件（复用面板 SMTP 配置，24h 内容去重）
frpp check all --fix                       # 一键修复（默认仅列出；加 --destructive 实际执行）
frpp check all --history-dir /var/log/frpp-history   # 报告历史目录（保留 30 份，终端尾部附上次对比）

frpp check diff old.json new.json          # 对比两份报告（新增失败时退出码非 0）
frpp check report merge a.json b.json --out overview.json --format markdown   # 多节点聚合
```

### CLI 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `[group...]` | `all` | 任意分组组合 |
| `--format` | `text` | `text` / `json` / `markdown` / `prometheus` |
| `--out` | 空（仅终端） | 报告写入文件（0600 权限） |
| `--fail-on` | `major` | `info` / `minor` / `major` / `critical` |
| `--timeout` | `2m` | 全局超时 |
| `--checker-timeout` | `15s` | 单项超时（runOne 看门狗强制生效） |
| `--concurrency` | `4` | 检查并发度（errgroup.SetLimit） |
| `--destructive` | `false` | 允许写操作用例（结束后自动清理）；同时使 `--fix` 实际执行 |
| `--node` | 空 | 定向节点过滤 |
| `--login-user/-password` | 空 | JWT 检查用凭证（也可用 `CHECKER_ADMIN_USER/PASSWORD`） |
| `--perf-duration` / `--perf-concurrency` | `3s` / `4` | perf 基线参数 |
| `--no-color` / `--list` | `false` | 输出控制 |
| `--only` / `--skip` | 空 | 单项过滤：`group`、`group/name` 或裸检查项名（如 `db_migration`） |
| `--notify-mail` | 空 | 失败/告警摘要发往指定邮箱（复用面板 SMTP 配置；同一内容集合 24h 去重） |
| `--history-dir` | `check-history` | 报告历史目录（滚动保留 30 份；启用上次对比与通知去重） |
| `--fix` | `false` | 一键修复：孤儿记录清理/sort_index 归一化/废弃环境变量清单；默认仅列出，`--destructive` 实际执行 |

### 结论与退出码

每个检查项输出 `pass / fail / warn / skip`；`fail` 项的 `severity` 达到 `--fail-on` 门槛时进程退出码为 `1`。`skip` 有明确原因（平台不支持、缺凭证、未开 `--destructive`），保证报告可审计。

## 五、覆盖情况与未覆盖项

按任务要求逐项核对（✅=已覆盖，⚠️=旁路推断，❌=未覆盖及原因）：

**环境与配置** ✅ 全部（`.env`、密钥强度、URL 一致性、DSN 连接、schema 抽查、四端口监听）
**认证与安全** ✅ 全部（注册门禁、登录链路、Cookie 属性、JWT 伪造/过期、frps 插件、cert 端点）；⚠️ 越权访问其他 TenantID 资源：受保护矩阵验证匿名拒绝，跨租户越权需要第二个租户的真实账号，未构造（避免创建测试用户）
**Master 控制面** ⚠️ 节点注册表经 `clientsstatus` + DB 对账推断（旁路进程无法读取 `GetClientsManager` 内存）；✅ `EVENT_PING` 延迟分布；❌ gRPC ServerReflection/health check：master 未注册 reflection 或 health service（已 grep 确认），改用一元 RPC `PullClientConfig` 存活探测；❌ freecache 命中率：freecache 未暴露命中率统计，仅校验 `MASTER_CACHE_SIZE` 配置
**Server/frps** ✅ 配置解析、端口冲突、`/auth` 宿主、统计单调性；❌ 热重载不中断验证：需要向运行中的 frps 下发配置并观察连接保持，属破坏性长流程，仅提供 `--destructive` 的 E2E 入口；✅ 高级模式原始配置合入（DB 中保存的即为合入后的 `v1.ServerConfig`，解析+端口提取+multiuser 插件核对）
**Client/frpc** ✅ 配置解析、三种 transport 实测、节点心跳、兼容能力（cert+插件）；⚠️ 端到端隧道连通（tcp/udp/http/... 各一例）需在线节点，`--destructive` 用例仅验证创建/下发/清理链路；✅ `natHoleSTUNServer` UDP 探测；❌ 断线重连回归：需主动杀节点进程，不满足"只读"前提；⚠️ `MASTER_COMPATIBLE_MODE` 在当前代码中不存在（见第二节说明）
**Worker** ✅ DB 完整性、二进制、代码漂移、ingress 引用；❌ workerd 进程存活/崩溃重启策略：`WorkersManager` 在 client 进程内存中，旁路只能经 `/api/v1/worker/status`（需凭证+在线节点），已留接口
**WireGuard** ✅ 地址规划、拓扑完整性、relay 公网约束、密钥、端口冲突；✅（仅 linux）wgctrl 设备探测：接口/peer/握手/流量；⚠️ UDP 与 WebSocket 传输分别打通：依赖真实对端节点，DB 层校验 `listen_port`/`ws_listen_port` 配置，实际握手经 linux 设备探测的 `latest handshake` 判定；❌ 多跳最短路径切换演练：需 ≥3 台真实节点且主动断链，超出只读范畴；❌ windows/macos 设备级探测：wgctrl 内核接口不支持（fork 已有 `wireguard_manager_windows.go` 用户态实现，握手状态请用面板 `/api/v1/wg/runtime/get`）
**WebUI** ✅ 首页/静态资源/日志流鉴权/pb 漂移/版本错位；❌ 浏览器端 WebSocket 日志流推流与重连：需要真实浏览器会话与在线节点，静态端点鉴权已覆盖
**数据与一致性** ✅ 孤儿记录（proxy/worker/wg）、软删除审计、流量对账、schema 抽查
**可观测性** ✅ OTA（`upgrade.ReadStatus` + 版本错位）、pprof 端点、日志级别配置；❌ 日志轮转（log.maxDays）：日志走 stdout（logrus），无本地日志文件可扫；❌ 优雅退出子进程回收验证：需要向运行中进程发 SIGTERM 并观测，无法旁路
**性能** ✅ API 基线（QPS/P99）、gRPC RTT；⚠️ 100/500/1000 节点注册压测与单节点 100+ 隧道下发耗时：会真实创建节点记录并推送生产节点，超出 `--destructive` 的"用后即清"边界，作为后续扩展点（见第七节）

## 六、Drone CI 接入

在 `.drone.yaml` 追加（或独立 pipeline）：

```yaml
kind: pipeline
name: frpp-check
steps:
  - name: build
    image: golang:1.24
    commands:
      - go build -o /tmp/frpp ./cmd/frpp

  - name: self-check
    image: golang:1.24
    environment:
      CHECKER_ADMIN_USER: admin
      CHECKER_ADMIN_PASSWORD:
        from_secret: frpp_admin_password
    commands:
      # 对 staging master 跑安全与控制面检查；major 及以上失败使流水线变红
      - /tmp/frpp check env auth master --fail-on major
        --timeout 2m --format json --out /tmp/report.json
      - cat /tmp/report.json
```

GitHub Actions 等价片段：

```yaml
- name: frpp self-check
  run: |
    go build -o frpp ./cmd/frpp
    ./frpp check env auth master --fail-on major --format json --out report.json
- uses: actions/upload-artifact@v4
  if: always()
  with: { name: checker-report, path: report.json }
```

定时巡检（crontab）：

```cron
0 6 * * * /usr/local/bin/frpp check all --format json --out /var/log/frpp/report-$(date +\%F).json --fail-on major >> /var/log/frpp/check.log 2>&1
```

## 七、已知限制与后续扩展

1. **旁路视角**：节点注册表、freecache、WorkersManager 等进程内状态只能通过 REST/DB 推断；如需进程内精确检查，可后续在 master 增加 `--check-internal` 模式（与 master 同进程注入 Checker，接口已按 `*Env` 抽象，可直接复用）。
2. **wg 设备探测仅 linux**：其他平台 Skip 并提示 `APP_USE_GVISOR_NET` 用户态实现；windows 可后续接入 `wgctrl` 的 wintun 路径。
3. **perf 大规模注册压测未实现**：需在 `--destructive` 下批量 `EVENT_REGISTER_CLIENT` + gRPC 双向流，建议在独立 staging master 上做，实现入口已预留（`group_perf.go`）。
4. **破坏性 E2E 依赖面板 API 的 JSON 字段**：`client/tunnel_e2e` 使用 JSON 协议创建隧道，若面板校验调整需同步更新请求体。
5. ~~**报告聚合**：多节点巡检目前各自产出 report.json，后续可加 `check report merge` 子命令聚合为总览。~~ **已实现**（2026-11-15）：`frpp check report merge`，报告 host 字段为节点标识；多节点巡检配合定时任务即可产出总览。

## 八、对现有仓库的改动清单

**新增（全部为独立目录，不影响既有逻辑）：**

```
cmd/frpp/shared/check.go              # NewCheckCmd：frpp check 子命令与 CLI 参数
internal/checker/checker.go           # Checker 接口 / Level / Status / Result
internal/checker/env.go               # Env：配置、HTTP、懒加载 DB 与登录态
internal/checker/runner.go            # errgroup 并发执行、单项超时看门狗、panic 防护
internal/checker/registry.go          # 分组注册表与 --only/--skip 过滤
internal/checker/output.go            # text(彩色)/json/markdown/prometheus 渲染 + 统一脱敏
internal/checker/diff.go              # 报告对比（check diff）与 --history-dir 管理
internal/checker/merge.go             # 多节点报告聚合（check report merge）
internal/checker/notify.go            # --notify-mail 邮件告警（复用面板 SMTP 与统一模板）
internal/checker/fix.go               # --fix 一键修复（孤儿清理/排序归一化/废弃变量清单）
internal/checker/util_redact.go       # 敏感信息脱敏（URL userinfo / mysql DSN / 键值口令）
internal/checker/statfs_unix.go       # 磁盘用量探测（//go:build !windows）
internal/checker/statfs_windows.go    # 同上，windows 降级
internal/checker/util_http.go         # protobuf/JSON 探测客户端
internal/checker/util_db.go           # 只读 DB 打开（sqlite mode=ro）
internal/checker/util_net.go          # TCP/UDP 探测、公网 IP 判定
internal/checker/group_env.go         # env 组 14 项
internal/checker/group_auth.go        # auth 组 9 项
internal/checker/group_master.go      # master 组 8 项
internal/checker/group_server.go      # server 组 5 项
internal/checker/group_client.go      # client 组 7 项
internal/checker/group_worker.go      # worker 组 4 项
internal/checker/group_wg.go          # wg 组 6 项（跨平台）
internal/checker/wgdevice_linux.go    # wgctrl 设备探测（//go:build linux）
internal/checker/wgdevice_other.go    # 非 linux 降级实现
internal/checker/group_webui.go       # webui 组 5 项
internal/checker/group_data.go        # data 组 10 项
internal/checker/group_perf.go        # perf 组 3 项
docs/checker.md                       # 本文档
docs/checker-report.example.json      # 真实运行的 JSON 报告样例
docs/checker-report.example.md        # Markdown 报告样例
```

**修改（2 个文件，均为 1 行级改动）：**

```diff
--- a/cmd/frpp/shared/cmd.go          # BuildCommand 注册 check 子命令
+++ b/cmd/frpp/shared/cmd.go
@@
 		NewServerCmd(cfg),
+		NewCheckCmd(cfg),
 		NewJoinCmd(),
```

```diff
--- a/go.mod / go.sum                 # go mod tidy：补 linux 构建依赖
+++ b/go.mod
@@
-	github.com/pion/stun/v3 v3.0.2          # 已无引用，tidy 移除
+	golang.org/x/sync v0.17.0               # errgroup（原为 indirect，转直接依赖）
+	github.com/mdlayher/genetlink v1.3.2    # wgctrl linux 路径依赖（indirect）
+	github.com/mdlayher/netlink v1.7.2      # 同上
```

> `biz/`、`services/`、`models/` 等既有逻辑**零改动**。
