# frp-panel check report

- 版本: `dev-build`
- 角色: `master`
- 开始时间: 2026-11-14T00:44:27+08:00
- 总耗时: 10.0ms
- 分组: env, auth, master

| 组 | 检查项 | 级别 | 结论 | 耗时 | 说明 |
|---|---|---|---|---|---|
| auth | `jwt_expired` | critical | fail | 1.0ms | 请求失败: POST http://127.0.0.1:9000/api/v1/user/get: Post "http://127.0.0.1:9000/api/v1/user/get": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `jwt_forged` | critical | fail | 0s | 请求失败: POST http://127.0.0.1:9000/api/v1/user/get: Post "http://127.0.0.1:9000/api/v1/user/get": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `login_invalid` | critical | fail | 774.2µs | 登录接口不可达: POST http://127.0.0.1:9000/api/v1/auth/login: Post "http://127.0.0.1:9000/api/v1/auth/login": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `cert_reject` | major | fail | 995.2µs | cert 接口不可达: POST http://127.0.0.1:9000/api/v1/auth/cert: Post "http://127.0.0.1:9000/api/v1/auth/cert": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `frp_plugin_reject` | major | fail | 1.0ms | /auth 插件端点不可达: POST http://127.0.0.1:9000/auth: Post "http://127.0.0.1:9000/auth": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `protected_matrix` | major | fail | 5.0ms | REST API 完全不可达，无法验证鉴权矩阵（先解决 env/port_listening） |
| auth | `register_gate` | major | fail | 3.2ms | 注册接口不可达: POST http://127.0.0.1:9000/api/v1/auth/register: Post "http://127.0.0.1:9000/api/v1/auth/register": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| auth | `frp_plugin_accept` | minor | skip | 995.2µs | users 表无带 token 的用户 |
| auth | `login_valid` | minor | skip | 0s | 未提供登录凭证（--login-user/--login-password） |
| env | `db_connect` | critical | fail | 10.0ms | 缺少数据表: endpoints, clients, users, workers, wireguards, networks, wireguard_links, servers, proxy_config, proxy_stats, history_proxy_stats（DB 指向了错误的库或版本不匹配） |
| env | `global_secret` | critical | fail | 0s | APP_GLOBAL_SECRET 长度仅 6，强度不足 |
| env | `config_source` | major | pass | 0s | 发现 1 个配置文件 |
| env | `node_identity` | major | pass | 6.0ms | 节点身份已配置 |
| env | `ota_status` | major | pass | 0s | 无 OTA 升级状态记录（从未执行过 frpp upgrade） |
| env | `port_listening` | major | fail | 4.0ms | 3 个端口未监听 |
| env | `profiler` | major | pass | 0s | pprof profiler 未开启 |
| env | `rpc_reachability` | major | warn | 0s | MASTER_RPC_HOST=127.0.0.1 为回环/私网地址，远程节点将无法接入 |
| env | `url_consistency` | major | pass | 0s | URL 配置一致 |
| env | `cache_size` | minor | pass | 0s | MASTER_CACHE_SIZE=10MB |
| env | `deprecated_vars` | minor | pass | 0s | 未发现已失效的旧版环境变量 |
| env | `logging` | minor | pass | 0s | 日志配置合法 |
| master | `grpc_tls` | critical | fail | 998.9µs | gRPC over TLS 探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it." |
| master | `grpc_ws` | major | fail | 998.2µs | ws 传输探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it.\nwsgrpc dialer error\nwsgrpc error" |
| master | `node_status` | major | skip | 0s | 未提供登录凭证，无法查询节点在线状态与 PING 延迟 |
| master | `wsgrpc_route` | major | fail | 0s | /wsgrpc 不可达: GET http://127.0.0.1:9000/wsgrpc: Get "http://127.0.0.1:9000/wsgrpc": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| master | `grpc_insecure` | minor | warn | 998.9µs | 明文 gRPC 探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it."（若生产为纯 TLS 部署属正常） |
| master | `grpc_wss` | minor | fail | 998.2µs | wss 传输探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it.\nwsgrpc dialer error\nwsgrpc error" |
| master | `json_compat` | minor | skip | 0s | 未提供登录凭证，跳过 JSON 协议兼容检查 |

## 详情

### auth/jwt_expired — fail (critical)

请求失败: POST http://127.0.0.1:9000/api/v1/user/get: Post "http://127.0.0.1:9000/api/v1/user/get": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: 过期 token 仍可用说明 jwt v5 的 exp 校验未生效，检查 utils.ParseToken 与中间件版本

> 相关配置: APP_COOKIE_AGE, utils.ParseToken


### auth/jwt_forged — fail (critical)

请求失败: POST http://127.0.0.1:9000/api/v1/user/get: Post "http://127.0.0.1:9000/api/v1/user/get": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: 若伪造 token 通过校验，说明 APP_GLOBAL_SECRET 与 JWT 派生链路异常，立即更换密钥并全员重新登录

> 相关配置: APP_GLOBAL_SECRET, conf.JWTSecret


### auth/login_invalid — fail (critical)

登录接口不可达: POST http://127.0.0.1:9000/api/v1/auth/login: Post "http://127.0.0.1:9000/api/v1/auth/login": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: 若错误口令能登录，检查登录处理逻辑与数据库口令哈希（utils.HashPassword/bcrypt）

> 相关配置: /api/v1/auth/login, users.password(bcrypt)


### auth/cert_reject — fail (major)

cert 接口不可达: POST http://127.0.0.1:9000/api/v1/auth/cert: Post "http://127.0.0.1:9000/api/v1/auth/cert": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: /api/v1/auth/cert 用错误 secret 必须拒绝，否则任意节点可伪造证书接入 gRPC

> 相关配置: /api/v1/auth/cert, CLIENT_SECRET


### auth/frp_plugin_reject — fail (major)

/auth 插件端点不可达: POST http://127.0.0.1:9000/auth: Post "http://127.0.0.1:9000/auth": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: frps multiuser 认证插件必须拒绝非法 token；检查 /auth 端点与 freecache 初始化

> 相关配置: POST /auth（frps multiuser HTTP 插件）, SERVER_API_PORT


### auth/protected_matrix — fail (major)

REST API 完全不可达，无法验证鉴权矩阵（先解决 env/port_listening）

| 明细 |
|---|
| user /api/v1/user/get — 网络错误: POST http://127.0.0.1:9000/api/v1/user/get: Post "http://127.0.0.1:9000/api/v1/user/get": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| platform /api/v1/platform/clientsstatus — 网络错误: POST http://127.0.0.1:9000/api/v1/platform/clientsstatus: Post "http://127.0.0.1:9000/api/v1/platform/clientsstatus": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| client /api/v1/client/list — 网络错误: POST http://127.0.0.1:9000/api/v1/client/list: Post "http://127.0.0.1:9000/api/v1/client/list": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| server /api/v1/server/list — 网络错误: POST http://127.0.0.1:9000/api/v1/server/list: Post "http://127.0.0.1:9000/api/v1/server/list": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| frpc /api/v1/frpc/update — 网络错误: POST http://127.0.0.1:9000/api/v1/frpc/update: Post "http://127.0.0.1:9000/api/v1/frpc/update": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| frps /api/v1/frps/update — 网络错误: POST http://127.0.0.1:9000/api/v1/frps/update: Post "http://127.0.0.1:9000/api/v1/frps/update": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| proxy /api/v1/proxy/list_configs — 网络错误: POST http://127.0.0.1:9000/api/v1/proxy/list_configs: Post "http://127.0.0.1:9000/api/v1/proxy/list_configs": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| worker /api/v1/worker/list — 网络错误: POST http://127.0.0.1:9000/api/v1/worker/list: Post "http://127.0.0.1:9000/api/v1/worker/list": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| wg /api/v1/wg/list — 网络错误: POST http://127.0.0.1:9000/api/v1/wg/list: Post "http://127.0.0.1:9000/api/v1/wg/list": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| streamlog /api/v1/log — 网络错误: POST http://127.0.0.1:9000/api/v1/log: Post "http://127.0.0.1:9000/api/v1/log": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| pty /api/v1/pty/frpp-check-node — 网络错误: POST http://127.0.0.1:9000/api/v1/pty/frpp-check-node: Post "http://127.0.0.1:9000/api/v1/pty/frpp-check-node": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |

> **修复建议**: 受保护路由对匿名请求必须返回 401；出现 200/404 说明中间件挂载不完整

> 相关配置: middleware.JWTAuth -> AuthCtx -> RBAC, biz/master/handler.go


### auth/register_gate — fail (major)

注册接口不可达: POST http://127.0.0.1:9000/api/v1/auth/register: Post "http://127.0.0.1:9000/api/v1/auth/register": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: 生产环境保持 APP_ENABLE_REGISTER=false；首管理员注册完成后务必关闭

> 相关配置: APP_ENABLE_REGISTER, /api/v1/auth/register


### auth/frp_plugin_accept — skip (minor)

users 表无带 token 的用户

> 相关配置: POST /auth, users.token, MASTER_CACHE_SIZE


### auth/login_valid — skip (minor)

未提供登录凭证（--login-user/--login-password）

> 相关配置: APP_COOKIE_NAME, APP_COOKIE_AGE, APP_COOKIE_SECURE, APP_COOKIE_HTTP_ONLY, APP_COOKIE_PATH, APP_COOKIE_DOMAIN


### env/db_connect — fail (critical)

缺少数据表: endpoints, clients, users, workers, wireguards, networks, wireguard_links, servers, proxy_config, proxy_stats, history_proxy_stats（DB 指向了错误的库或版本不匹配）

> **修复建议**: 检查 DB_TYPE 与 DB_DSN：sqlite 确认路径与权限；mysql/postgres 确认 DSN、网络与账号

> 相关配置: DB_TYPE, DB_DSN


### env/global_secret — fail (critical)

APP_GLOBAL_SECRET 长度仅 6，强度不足

> **修复建议**: 设置强随机的 APP_GLOBAL_SECRET（>=32 字符），修改后需同步重新加入所有节点

> 相关配置: APP_GLOBAL_SECRET, conf.JWTSecret = SHA1(MASTER_API_HOST:MASTER_API_PORT:APP_GLOBAL_SECRET)


### env/config_source — pass (major)

发现 1 个配置文件

| 明细 |
|---|
| 已加载: .env (size=53, mode=-rw-rw-rw-) |
| 不存在: /etc/frpp/.env |


### env/node_identity — pass (major)

节点身份已配置

| 明细 |
|---|
| CLIENT_ID=R9000P |

> 相关配置: CLIENT_ID, CLIENT_SECRET


### env/ota_status — pass (major)

无 OTA 升级状态记录（从未执行过 frpp upgrade）

| 明细 |
|---|
| open E:\Temp\vaala-frp-panel-upgrade\status.json: The system cannot find the path specified. |


### env/port_listening — fail (major)

3 个端口未监听

| 明细 |
|---|
| master REST API :9000 (MASTER_API_PORT) — 不可达: dial 127.0.0.1:9000: dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it. |
| master RPC (gRPC/TLS mux) :9001 (MASTER_RPC_PORT) — 不可达: dial 127.0.0.1:9001: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it. |
| server API (frps auth 插件宿主) :8999 (SERVER_API_PORT) — 不可达: dial 127.0.0.1:8999: dial tcp 127.0.0.1:8999: connectex: No connection could be made because the target machine actively refused it. |

> **修复建议**: 确认对应进程已启动；云主机还需在防火墙/安全组放行 9000(REST)、9001(gRPC)、8999(server API)

> 相关配置: MASTER_API_PORT, MASTER_RPC_PORT, SERVER_API_PORT


### env/rpc_reachability — warn (major)

MASTER_RPC_HOST=127.0.0.1 为回环/私网地址，远程节点将无法接入

> 相关配置: MASTER_RPC_HOST, MASTER_RPC_PORT, CLIENT_RPC_URL


### env/logging — pass (minor)

日志配置合法

| 明细 |
|---|
| LOGGER_DEFAULT_LOGGER_LEVEL=info ✓ |
| LOGGER_FRP_LOGGER_LEVEL=info ✓ |

> 相关配置: LOGGER_DEFAULT_LOGGER_LEVEL, LOGGER_FRP_LOGGER_LEVEL, IS_DEBUG


### master/grpc_tls — fail (critical)

gRPC over TLS 探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it."

> **修复建议**: 检查 MASTER_RPC_PORT 是否监听、TLS 证书链（conf.GetCertTemplate 自签 CA）与 MASTER_RPC_HOST 一致性

> 相关配置: MASTER_RPC_HOST, MASTER_RPC_PORT, CLIENT_TLS_RPC


### master/grpc_ws — fail (major)

ws 传输探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it.\nwsgrpc dialer error\nwsgrpc error"

> **修复建议**: ws 传输依赖 API 端口的 /wsgrpc 路由；检查反向代理是否放行该路径与升级头

> 相关配置: CLIENT_RPC_URL, MASTER_API_HOST, MASTER_API_PORT, /wsgrpc


### master/node_status — skip (major)

未提供登录凭证，无法查询节点在线状态与 PING 延迟

> 相关配置: /api/v1/platform/clientsstatus, EVENT_PING, defs.OfflineCheckDelay


### master/wsgrpc_route — fail (major)

/wsgrpc 不可达: GET http://127.0.0.1:9000/wsgrpc: Get "http://127.0.0.1:9000/wsgrpc": dial tcp 127.0.0.1:9000: connectex: No connection could be made because the target machine actively refused it.

> **修复建议**: runMaster 中注册于 MasterRouter(GET /wsgrpc)；404 说明路由未挂载或请求打到了别的服务

> 相关配置: GET /wsgrpc（无升级头）


### master/grpc_insecure — warn (minor)

明文 gRPC 探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it."（若生产为纯 TLS 部署属正常）

> 相关配置: MASTER_RPC_PORT, CLIENT_TLS_RPC


### master/grpc_wss — fail (minor)

wss 传输探测失败: PullClientConfig: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9001: connectex: No connection could be made because the target machine actively refused it.\nwsgrpc dialer error\nwsgrpc error"

> **修复建议**: wss 需要 API 端口前置 HTTPS 反代或 TLS；检查证书有效性

> 相关配置: CLIENT_RPC_URL, MASTER_API_HOST, MASTER_API_PORT, /wsgrpc


### master/json_compat — skip (minor)

未提供登录凭证，跳过 JSON 协议兼容检查

> 相关配置: common.GetProtoRequest（application/json 分支）


## 汇总

| 状态 | 数量 |
|---|---|
| pass | 8 |
| fail | 14 |
| warn | 2 |
| skip | 4 |
