# FRP-Panel

FRP-Panel 是一款基于 FRP 的可视化管理面板，提供中心化配置、统一凭证、动态调度和边缘 Worker 支持，让内网穿透和服务暴露更简单、更安全、更高效。

[详细使用文档 (Wiki)](https://fengmuxi.github.io/frp-panel/) | [Frp-panel Blog 开发记录](https://vaala.cat/posts/frp-panel-doc/) | [截图/视频展示](https://fengmuxi.github.io/frp-panel/screenshots) | QQ 群：830620423

> [WireGuard 多跳智能组网功能开发笔记](https://vaala.cat/posts/frp-panel-with-wireguard/) 点开有好玩的 Demo 哦！

中文文档 | [English](./README.md)

<div align="center">
<a href="https://trendshift.io/repositories/7147" target="_blank"><img src="https://trendshift.io/api/badge/repositories/7147" alt="VaalaCat%2Ffrp-panel | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/></a>
</div>


## 核心优势

| 优势                       | 描述                                                         |
|:--------------------------|:------------------------------------------------------------|
| 中央化配置                 | 所有客户端/服务端配置由 Master 管理，无需手动编辑 JSON 文件          |
| 多节点统一管理             | 支持任意数量的 frpc（客户端）与 frps（服务端）节点集中监控与调度     |
| 可视化界面                 | Web UI 一键创建、编辑、监控隧道和Worker，实时日志与统计一目了然                  |
| 简化凭证分发               | 自动生成并分发启动命令，无须手动传参             |
| 边缘 Worker 自部署         | 在 Client 上部署自定义 Worker，Server 将其暴露到公网，Master 可实时调整配置 |
| WireGuard 智能组网         | 支持 Client 相互之间使用 WireGuard over UDP/Websocket 多跳组网、自定义路由、拓扑，按延迟和带宽智能计算最短路由 |

> 组网功能目前处于测试阶段，可能存在一些问题，欢迎反馈

## 架构概览

![arch](docs/public/images/arch.png)

1. **Master** – 集中管理与鉴权，要求所有 Server 和 Client 可访问；
2. **Server** – 承载业务流量，作为公网入口，为 Client 提供服务；
3. **Client** – 内网代理，支持部署 Worker，支持 WireGuard 与其他 Client 智能组网；

> 组网目前要求网络中至少有一个 Client 有公网IP作为中继节点，且目前仅支持 Linux 操作系统组网

## 项目目录结构

```text
frp-panel/
├── cmd/                        # 程序入口
│   ├── frpp/                   #   主二进制：master / client / server 子命令（shared/ 存放子命令实现，out/ 为嵌入的前端产物）
│   └── frppc/                  #   精简版客户端二进制（仅 client 相关命令）
├── biz/                        # HTTP/RPC 接口处理层
│   ├── master/                 #   Master 接口与路由注册（handler.go）
│   ├── client/                 #   Client 端接口
│   ├── server/                 #   Server 端接口
│   └── common/                 #   跨端共享接口
├── services/                   # 业务服务层
│   ├── master/ client/ server/ #   三端核心服务逻辑
│   ├── tunnel/ workerd/ wg/    #   隧道、边缘 Worker、WireGuard 组网
│   ├── rpc/ clientrpc/ mux/    #   gRPC 通信
│   └── api/ dao/ cache/ rbac/ watcher/ app/
├── models/                     # 数据模型与数据库访问（auth、user、proxy、shell、wg 等）
├── common/                     # 公共代码（统一响应 common/response.go）
├── conf/                       # 配置加载，读取 .env（settings.go）
├── defs/                       # 常量与类型定义（TraceIDKey 等）
├── middleware/                 # HTTP 中间件
├── utils/                      # 通用工具（logger、pty、wsgrpc）
├── idl/                        # Protobuf 接口定义（.proto 源文件）
├── pb/                         # 生成的 Go 代码（codegen.sh 产物，勿手改）
├── www/                        # Next.js 前端
│   ├── pages/ components/      #   页面与组件
│   ├── api/ store/ hooks/      #   请求、状态与 hooks
│   ├── lib/pb/                 #   生成的 TypeScript pb 代码（勿手改）
│   └── i18n/ styles/ types/ config/ public/
├── docs/                       # 文档站点（VitePress：docs/en、docs/public）
├── build.sh / codegen.sh       # 构建 / proto 代码生成脚本
├── Dockerfile* / install.*     # 容器化与安装脚本
└── .env.sample                 # 配置样例
```

## 文档

完整文档位于 [`docs/`](./docs) 目录（VitePress 站点）：

- [快速开始](./docs/quick-start.md)
- 部署指南：[Master](./docs/deploy-master.md) · [Server](./docs/deploy-server.md) · [Client](./docs/deploy-client.md)
- [WireGuard 多跳智能组网](./docs/wireguard.md)
- [函数功能（边缘 Worker）](./docs/worker.md)
- [邮件通知](./docs/mail-notification.md)
- [全部配置项说明](./docs/all-configs.md)
- [CI/CD 与发布流程](./docs/ci-cd.md)
- [贡献指南](./docs/contribute.md)
- [常见问题 FAQ](./docs/faq.md)
- [截图展示](./docs/screenshots.md)

在线版本 → [FRP-Panel Wiki](https://fengmuxi.github.io/frp-panel/)

## 本地开发

### 环境要求

- Go **1.24+**（toolchain go1.24.1）
- Node.js **20+** 及 **pnpm**
- `protoc` 及插件（`protoc-gen-go`、`protoc-gen-go-grpc`、`@protobuf-ts/plugin`）—— 仅在修改 `.proto` 文件时需要

### 配置

配置从当前目录的 `.env` 文件读取（所有配置项及说明见 [conf/settings.go](./conf/settings.go)）：

```bash
cp .env.sample .env
```

本地常用配置示例：

```dotenv
# 必填：>=16 位强随机串，生成 `openssl rand -hex 32`；为空或默认值 frp-panel 时 master 拒绝启动
APP_GLOBAL_SECRET="dev-only-0123456789abcdef"
MASTER_RPC_HOST=127.0.0.1       # Client/Server 连接 Master 的 RPC 地址
# 本地纯 HTTP 调试必填（默认 true，浏览器不会回传 Cookie 导致登录态异常）；生产走 HTTPS 反代请省略
APP_COOKIE_SECURE=false
DB_TYPE=sqlite3                 # 默认 SQLite，无需独立数据库服务
DB_DSN=./data.db?_pragma=journal_mode(WAL)   # 默认为 /data/data.db，请改成本地路径
IS_DEBUG=true                   # 调试模式：trace 级日志 + pprof 性能分析（端口 6961）
```

> 变量名前缀（`APP_` / `MASTER_` / `DB_` / `CLIENT_`）不能省略，程序按 `env-prefix` 拼接读取，漏写前缀会被静默忽略。
> 完整的必填项与默认值见 [docs/deploy-master.md](./docs/deploy-master.md) 与 [docs/all-configs.md](./docs/all-configs.md)。

### 启动 Master（控制平面）

```bash
go run ./cmd/frpp master
```

- Web UI / API：`http://localhost:9000`（MASTER_API_PORT，默认 9000）
- gRPC 端点：`:9001`（MASTER_RPC_PORT，默认 9001）
- 首个管理员通过登录页注册；如需开放后续注册，设置 `APP_ENABLE_REGISTER=true`

### 前端开发（热更新）

```bash
cd www
pnpm install
pnpm dev
```

开发模式下，Next.js 会将 `/api/*` 代理到 `http://localhost:9000`（见 [www/next.config.js](./www/next.config.js)），即直连上面启动的 Master。浏览器访问 `http://localhost:3000`。

如需将前端构建并嵌入 Go 二进制（静态导出复制到 `cmd/frpp/out` 后由 Go embed 嵌入）：

```bash
cd www && pnpm build
```

### 启动 Client / Server 节点

复制 Master Web UI 中生成的启动命令（包含生成的 ID/Secret），或手动执行：

```bash
go run ./cmd/frpp client -i <clientID> -s <clientSecret>
go run ./cmd/frpp server -i <serverID> -s <serverSecret>
```

两个节点的所有配置均从 Master 拉取，无需本地配置文件。

### 构建、测试、Lint

```bash
bash ./build.sh                  # 完整构建：pnpm build + 交叉编译输出到 dist/
bash ./build.sh --skip-frontend  # 仅构建 Go 二进制

go test ./...                    # 单元测试（wg、workerd、utils、conf、models）
go mod tidy                      # 模块整理（CI 会检查是否有 diff）
cd www && pnpm run lint          # 前端 ESLint（CI 必须通过）
```

### 代码生成（修改 proto 文件后必须执行）

```bash
bash ./codegen.sh   # idl/*.proto -> pb/（Go）与 www/lib/pb（TypeScript）
```

请勿手改 `pb/`、`www/lib/pb/`、`cmd/frpp/out` 下的生成文件。

### 调试技巧

- **日志级别**：`LOGGER_DEFAULT_LOGGER_LEVEL`（frp-panel 内部）与 `LOGGER_FRP_LOGGER_LEVEL`（内嵌 frp），例如都设为 `debug`
- **调试模式**：`IS_DEBUG=true` 开启 trace 日志与 pprof 性能分析（`DEBUG_PROFILER_ENABLED`、`DEBUG_PROFILER_PORT`，默认 `6961` → `http://localhost:6961/debug/pprof/`）
- **Trace ID**：每个 API 响应都带有 `defs.TraceIDKey` trace 头，用该 ID 检索日志即可端到端跟踪一次请求
- **新 API 不生效？** 路由注册在 [biz/master/handler.go](./biz/master/handler.go)；新增响应类型还需加入 [common/response.go](./common/response.go) 的 `RespType` 联合类型
- 更多架构与开发说明：[docs/contribute.md](./docs/contribute.md)

## 社区与赞助

本项目完全开源，欢迎 Star、Issues、PR。
若 FRP-Panel 为您带来价值，欢迎赞助作者：

-  邮箱：me@vaala.cat

[NodeSupport](https://github.com/NodeSeekDev/NodeSupport) / [林枫云](https://www.dkdun.cn) 赞助了该项目

<div align="left">
  <a href="https://yxvm.com/">
    <img src="https://github.com/user-attachments/assets/0bd7087a-7994-4caf-a465-a428af19c5aa" width="300" />
  </a>
</div>
<div align="left">
  <a href="https://www.dkdun.cn">
    <img src="https://www.dkdun.cn/themes/web/www/upload/local68c2dbb2ab148.png" width="300" />
  </a>
</div>

## 项目状态

[![Star History](https://api.star-history.com/svg?repos=fengmuxi/frp-panel&type=Date)](https://www.star-history.com/#fengmuxi/frp-panel&Date)

---

*更多部署、使用与配置细节，请移步 Wiki → [FRP-Panel WiKi](https://fengmuxi.github.io/frp-panel/)*
