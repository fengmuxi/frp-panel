# 配置说明

## frp隧道高级模式配置

本面板完全兼容 frp 原本的`json`格式配置，仅需要将配置文件内容粘贴到服务端/客户端高级模式编辑框内，更新即可，详细的使用参考：[frp 文档](https://gofrp.org/zh-cn/docs/features/common/configure/)

## 程序启动配置文件

程序会按顺序读取以下文件内容作为配置文件：`.env`,`/etc/frpp/.env`

## 程序配置说明

> 文档可能有点老。。。
> 
> 完整的最新配置参考这个文件：[settings.go](https://github.com/fengmuxi/frp-panel/blob/main/conf/settings.go)
>
> **必填项与部署场景说明见 [Master 部署](./deploy-master.md)。** 变量名的 `APP_` / `MASTER_` / `DB_` / `CLIENT_` 前缀不可省略，
> 程序按 `env-prefix` 拼接读取，漏写前缀会被静默忽略。

| 类型   | 环境变量名                             | 默认值               | 描述                                                             |
|--------|-------------------------------------|--------------------|----------------------------------------------------------------|
| string | `APP_SECRET`                       | -                  | 应用密钥，用于客户端和服务器的和Master的通信加密                        |
| string | `APP_GLOBAL_SECRET`                | `frp-panel`（禁用） | **必填**：>=16 位强随机串（`openssl rand -hex 32`）。为空、过短或仍为默认值时 master 拒绝启动。派生 JWT 签名密钥与节点 secret，库内已有密文后更换必须走 `frpp migrate rekey` |
| bool   | `APP_REQUIRE_STRONG_SECRET`        | `true`             | 强钥校验开关：默认拒绝默认值/过短 `APP_GLOBAL_SECRET` 启动。**仅作旧部署过渡兼容**——设为 `false` 可让仍在使用默认密钥的库继续启动（`frpp check env` 会降级为警告），完成后应尽快 `frpp migrate rekey` 轮换并移除该开关；为空时始终拒绝 |
| int    | `APP_COOKIE_AGE`                   | `86400`            | Cookie 的有效期（秒），默认值为 1 天                                  |
| string | `APP_COOKIE_NAME`                  | `frp-panel-cookie` | Cookie 名称                                                        |
| string | `APP_COOKIE_PATH`                  | `/`                | Cookie 路径                                                       |
| string | `APP_COOKIE_DOMAIN`                | -                  | Cookie 域                                                         |
| bool   | `APP_COOKIE_SECURE`                | `true`             | Cookie 是否带 Secure 标记；纯 HTTP（无 TLS 反代）部署必须显式设为 `false`，否则登录态异常 |
| bool   | `APP_COOKIE_HTTP_ONLY`             | `true`             | Cookie 是否仅限 HTTP                                             |
| bool   | `APP_ENABLE_REGISTER`              | `false`            | 是否启用注册，仅允许第一个管理员注册                               |
| string | `MASTER_API_LISTEN_IP`             | `0.0.0.0`          | API/WebUI 监听 IP。**默认监听所有网卡（含公网）**；仅本机反代/本机访问时改为 `127.0.0.1` 可收紧暴露面（监听地址与 `MASTER_API_HOST` 不同——前者是绑定地址，后者是对外访问地址） |
| string | `MASTER_RPC_LISTEN_IP`             | `0.0.0.0`          | 节点 RPC（wsgrpc）监听 IP。**默认监听所有网卡（含公网）**；节点全部经域名反代/CDN 接入时改为 `127.0.0.1` 可收紧暴露面 |
| int    | `MASTER_API_PORT`                  | `9000`             | 主节点 API 端口                                                  |
| string | `MASTER_API_HOST`                  | -                  | 主节点域名，可以在反向代理和CDN后                                 |
| string | `MASTER_API_SCHEME`                | `http`             | 主节点 API 协议（注意，这里不影响主机行为，设置为https只是为了方便复制客户端启动命令，HTTPS需要自行反向代理）|
| int    | `MASTER_CACHE_SIZE`                | `10`               | 缓存大小（MB）                                                   |
| string | `MASTER_RPC_HOST`                  | `127.0.0.1`        | Master节点公共 IP 或域名                                          |
| int    | `MASTER_RPC_PORT`                  | `9001`             | Master节点 RPC 端口                                            |
| bool   | `MASTER_COMPATIBLE_MODE`           | `false`            | 兼容模式，用于官方 frp 客户端                                     |
| string | `MASTER_INTERNAL_FRP_SERVER_HOST`  | -                  | Master内置 frps 服务器主机，用于客户端连接                                |
| int    | `MASTER_INTERNAL_FRP_SERVER_PORT`  | `9002`             | Master内置 frps 服务器端口，用于客户端连接                                |
| string | `MASTER_INTERNAL_FRP_AUTH_SERVER_HOST` | `127.0.0.1`    | Master内置 frps 认证服务器主机                                          |
| int    | `MASTER_INTERNAL_FRP_AUTH_SERVER_PORT` | `8999`          | Master内置 frps 认证服务器端口                                          |
| string | `MASTER_INTERNAL_FRP_AUTH_SERVER_PATH` | `/auth`         | Master内置 frps 认证服务器路径                                          |
| int    | `SERVER_API_PORT`                  | `8999`             | 服务器 API 端口                                                  |
| string | `DB_TYPE`                          | `sqlite3`         | 数据库类型，如 mysql postgres 或 sqlite3 等                                 |
| string | `DB_DSN`                           | `data.db`         | 数据库 DSN，默认使用sqlite3，数据默认存储在可执行文件同目录下，对于 sqlite 是路径，其他数据库为 DSN，参见 [MySQL DSN](https://github.com/go-sql-driver/mysql#dsn-data-source-name) |
| string | `CLIENT_ID`                        | -                  | 客户端 ID                                                        |
| string | `CLIENT_SECRET`                   | -                  | 客户端密钥                                                       |
| bool   | `IS_DEBUG`                         | `false`            | 是否开启调试模式（影响日志/部分组件行为）                                  |
| bool   | `DEBUG_PROFILER_ENABLED`           | `false`            | 是否开启 profiler(pprof) HTTP 服务（默认仅监听 127.0.0.1）                 |
| int    | `DEBUG_PROFILER_PORT`              | `6961`             | profiler(pprof) HTTP 服务端口                                      |
