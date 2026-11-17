# FRP-Panel

FRP-Panel is a visualization management dashboard for FRP, offering centralized configuration, unified credentials, dynamic scheduling, and edge Worker support—making NAT traversal and service exposure simpler, safer, and more efficient.

[Detailed Documentation (Wiki)](https://fengmuxi.github.io/frp-panel/en/) · [Development Blog](https://vaala.cat/posts/frp-panel-doc/) · [Screenshots & Videos](https://fengmuxi.github.io/frp-panel/en/screenshots) · QQ Group: 830620423

> [WireGuard Multi-Hop Smart Networking Dev Notes](https://vaala.cat/posts/frp-panel-with-wireguard/) — includes a funny demo you can try.

English | [中文](./README_zh.md)

<div align="center">
  <a href="https://trendshift.io/repositories/7147" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/7147" alt="VaalaCat/frp-panel | Trendshift" width="250" height="55"/>
  </a>
</div>

## Key Advantages

| Advantage               | Description                                                                 |
|:-----------------------|:----------------------------------------------------------------------------|
| Centralized Configuration | All client/server configs are managed by Master—no manual JSON editing       |
| Multi-node Management     | Monitor and orchestrate any number of frpc (clients) and frps (servers)      |
| Visual Interface          | Create, edit, and monitor tunnels and Workers via Web UI, with real-time logs and stats |
| Simplified Credential Distribution | Auto-generate and distribute startup commands—no manual parameter passing |
| Edge Worker Deployment    | Deploy custom Workers on Clients, expose them via Server, and adjust configs live via Master |
| WireGuard Smart Networking | WireGuard over UDP/WebSocket with multi-hop routing, custom routes/topology, and latency + bandwidth-aware path selection |

> Networking is currently in beta. Please share feedback.  
> Networking requires at least one client with a public IP as a relay, and currently supports Linux systems only.

## Architecture Overview

![Architecture Diagram](./docs/public/images/arch.svg)

1. **Master** – Centralized management and authentication; requires access from all Servers and Clients  
2. **Server** – Public-facing entry point that handles traffic for Clients  
3. **Client** – Internal proxy that supports deploying Workers and WireGuard mesh networking  

## Project Structure

```text
frp-panel/
├── cmd/                        # Program entrypoints
│   ├── frpp/                   #   Main binary: master / client / server subcommands (shared/ holds command implementations, out/ is the embedded frontend build)
│   └── frppc/                  #   Slim client-only binary (client-related commands)
├── biz/                        # HTTP/RPC handler layer
│   ├── master/                 #   Master handlers and route registration (handler.go)
│   ├── client/                 #   Client-side interfaces
│   ├── server/                 #   Server-side interfaces
│   └── common/                 #   Cross-role shared interfaces
├── services/                   # Business service layer
│   ├── master/ client/ server/ #   Core logic for the three roles
│   ├── tunnel/ workerd/ wg/    #   Tunnels, edge Workers, WireGuard networking
│   ├── rpc/ clientrpc/ mux/    #   gRPC communication
│   └── api/ dao/ cache/ rbac/ watcher/ app/
├── models/                     # Data models and DB access (auth, user, proxy, shell, wg, ...)
├── common/                     # Shared code (unified response: common/response.go)
├── conf/                       # Config loading from .env (settings.go)
├── defs/                       # Constants and type definitions (TraceIDKey, ...)
├── middleware/                 # HTTP middleware
├── utils/                      # Utilities (logger, pty, wsgrpc)
├── idl/                        # Protobuf definitions (.proto sources)
├── pb/                         # Generated Go code (codegen.sh output, do not edit)
├── www/                        # Next.js frontend
│   ├── pages/ components/      #   Pages and components
│   ├── api/ store/ hooks/      #   Requests, state and hooks
│   ├── lib/pb/                 #   Generated TypeScript pb code (do not edit)
│   └── i18n/ styles/ types/ config/ public/
├── docs/                       # Documentation site (VitePress: docs/en, docs/public)
├── build.sh / codegen.sh       # Build / proto code generation scripts
├── Dockerfile* / install.*     # Containerization and install scripts
└── .env.sample                 # Config sample
```

## Documentation

The full documentation lives in [`docs/`](./docs) (built as a VitePress site):

- [Quick Start](./docs/quick-start.md)
- Deployment Guides: [Master](./docs/deploy-master.md) · [Server](./docs/deploy-server.md) · [Client](./docs/deploy-client.md)
- [WireGuard Multi-Hop Smart Networking](./docs/wireguard.md)
- [Functions (Edge Worker)](./docs/en/worker.md)
- [Email Notifications](./docs/en/mail-notification.md)
- [All Configuration Options](./docs/all-configs.md)
- [CI/CD and Release Process](./docs/en/ci-cd.md)
- [Contribution Guide](./docs/contribute.md)
- [FAQ](./docs/faq.md)
- [Screenshots](./docs/screenshots.md)

Online version → [FRP-Panel Wiki](https://fengmuxi.github.io/frp-panel/en/)

## Local Development

### Prerequisites

- Go **1.24+** (toolchain go1.24.1)
- Node.js **20+** and **pnpm**
- `protoc` + plugins (`protoc-gen-go`, `protoc-gen-go-grpc`, `@protobuf-ts/plugin`) — only needed when modifying `.proto` files

### Configuration

Configuration is loaded from a `.env` file in the current directory (see [conf/settings.go](./conf/settings.go) for all options and their descriptions):

```bash
cp .env.sample .env
```

Common local overrides:

```dotenv
# 必填：>=16 位强随机串，生成 `openssl rand -hex 32`；为空或默认值 frp-panel 时 master 拒绝启动
APP_GLOBAL_SECRET="dev-only-0123456789abcdef"
MASTER_RPC_HOST=127.0.0.1       # address for client/server RPC connections
# 本地纯 HTTP 调试必填（默认 true，浏览器不会回传 Cookie 导致登录态异常）；生产走 HTTPS 反代请省略
APP_COOKIE_SECURE=false
DB_TYPE=sqlite3                 # default, no separate DB service needed
DB_DSN=./data.db?_pragma=journal_mode(WAL)   # default is /data/data.db, point it to a local path
IS_DEBUG=true                   # debug mode: trace-level logs + pprof profiler (port 6961)
```

> 变量名前缀（`APP_` / `MASTER_` / `DB_` / `CLIENT_`）不能省略，程序按 `env-prefix` 拼接读取，漏写前缀会被静默忽略。
> 完整的必填项与默认值见 [docs/deploy-master.md](./docs/deploy-master.md) 与 [docs/all-configs.md](./docs/all-configs.md)。

### Start the Master (control plane)

```bash
go run ./cmd/frpp master
```

- Web UI / API: `http://localhost:9000` (MASTER_API_PORT, default 9000)
- gRPC endpoint: `:9001` (MASTER_RPC_PORT, default 9001)
- The first admin registers via the login page; set `APP_ENABLE_REGISTER=true` to allow further registrations

### Frontend development (hot reload)

```bash
cd www
pnpm install
pnpm dev
```

In dev mode, Next.js rewrites `/api/*` to `http://localhost:9000` (see [www/next.config.js](./www/next.config.js)), so the frontend dev server proxies straight to the Master started above. Open `http://localhost:3000`.

To build the frontend into the Go binary (static export copied to `cmd/frpp/out` and embedded):

```bash
cd www && pnpm build
```

### Start a Client / Server node

Copy the startup command shown in the Master Web UI (it contains the generated ID/Secret), or:

```bash
go run ./cmd/frpp client -i <clientID> -s <clientSecret>
go run ./cmd/frpp server -i <serverID> -s <serverSecret>
```

Both nodes pull all configuration from the Master — no local config files required.

### Build, test, lint

```bash
bash ./build.sh                  # full build: pnpm build + cross-compile to dist/
bash ./build.sh --skip-frontend  # Go binaries only

go test ./...                    # unit tests (wg, workerd, utils, conf, models)
go mod tidy                      # module tidy (CI checks for diffs)
cd www && pnpm run lint          # frontend ESLint (CI must pass)
```

### Code generation (after changing proto files)

```bash
bash ./codegen.sh   # idl/*.proto -> pb/ (Go) and www/lib/pb (TypeScript)
```

Never hand-edit generated files under `pb/`, `www/lib/pb/`, or `cmd/frpp/out`.

### Debugging tips

- **Log levels**: `LOGGER_DEFAULT_LOGGER_LEVEL` (frp-panel) and `LOGGER_FRP_LOGGER_LEVEL` (embedded frp), e.g. set both to `debug`
- **Debug mode**: `IS_DEBUG=true` enables trace logs and the pprof profiler (`DEBUG_PROFILER_ENABLED`, `DEBUG_PROFILER_PORT`, default `6961` → `http://localhost:6961/debug/pprof/`)
- **Trace ID**: every API response carries the `defs.TraceIDKey` trace header; grep logs by that ID to follow a request end-to-end
- **New API not showing up?** Routes are registered in [biz/master/handler.go](./biz/master/handler.go); response types must also be added to the `RespType` union in [common/response.go](./common/response.go)
- More architecture/dev notes: [docs/contribute.md](./docs/contribute.md)

## Community & Sponsorship

FRP-Panel is fully open source—welcome Stars, Issues, and PRs.  
If FRP-Panel brings you value, consider sponsoring the author:

- Email: me@vaala.cat

Sponsored by [NodeSupport](https://github.com/NodeSeekDev/NodeSupport) / [DartNode](https://dartnode.com) / [DK Cloud](https://www.dkdun.cn)

<div align="left">
  <a href="https://yxvm.com/">
    <img src="https://github.com/user-attachments/assets/0bd7087a-7994-4caf-a465-a428af19c5aa" width="300"/>
  </a>
  <div align="left">
  <a href="https://dartnode.com">
    <img src="https://dartnode.com/branding/DN-Open-Source-sm.png" width="300"/>
  </a>
</div>
  <div align="left">
  <a href="https://www.dkdun.cn">
    <img src="https://www.dkdun.cn/themes/web/www/upload/local68c2dbb2ab148.png" width="300"/>
  </a>
</div>
</div>

## Project Status

[![Star History](https://api.star-history.com/svg?repos=fengmuxi/frp-panel&type=Date)](https://www.star-history.com/#fengmuxi/frp-panel&Date)

---

For more deployment, usage, and configuration details, see the Wiki → [FRP-Panel Wiki](https://fengmuxi.github.io/frp-panel/en/)
