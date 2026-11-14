# FRP-Panel 开发代理指南（agent.md）

> 本文档面向 AI 编程代理/开发者，用于在 frp-panel 仓库中高效、规范地进行开发。
> 仓库：https://github.com/fengmuxi/frp-panel（上游：VaalaCat/frp-panel）

## 1. 项目概览

FRP-Panel 是基于 [frp](https://github.com/fatedier/frp) 的可视化管理面板，提供中心化配置、统一凭证、动态调度、边缘 Worker 与 WireGuard 智能组网。

- 语言/版本：Go 1.24（toolchain go1.24.1），前端 Next.js 14 + TypeScript + TailwindCSS
- 关键依赖：gin、gorm（sqlite/mysql/postgres）、go.uber.org/fx（依赖注入）、grpc/protobuf、logrus、casbin（RBAC）、golang-jwt、fatedier/frp、wireguard

### 三种角色

| 角色 | 职责 | 入口 |
|:--|:--|:--|
| **Master** | 控制平面：处理前端 API、集中管理配置与凭证，通过 gRPC stream 管理 Client/Server | [cmd/frpp](cmd/frpp/main.go) 子命令 `master` |
| **Server** | 公网入口，承载业务流量（frps + 连接 Master 的 RPC） | `frpp server` |
| **Client** | 内网代理，可部署 Worker、参与 WireGuard 组网（frpc + RPC） | [cmd/frppc](cmd/frppc/main.go) 或 `frpp client` |

Client/Server **不使用本地配置文件**，所有配置一律从 Master 拉取；同时保持与官方 frpc/frps 的兼容。

## 2. 目录结构

```
.
├── cmd/            # main 入口
│   ├── frpp/       # 主二进制（master/client/server 三合一），shared/ 下是命令构建与 fx 模块装配
│   └── frppc/      # 客户端精简二进制
├── biz/            # 业务逻辑层（HTTP handler / RPC 处理）
│   ├── master/     # 控制面 API：auth、client、server、proxy、user、worker、wg、shell、streamlog…
│   ├── client/     # 客户端角色逻辑
│   ├── server/     # 服务端角色逻辑
│   └── common/
├── services/       # 常驻内存服务（可启停的有状态模块）
│   ├── api/        # gin API 服务（需要外部传入 router）
│   ├── app/        # Application 抽象与 handler Wrapper（见 §5）
│   ├── master/     # master 的 gRPC 服务端，收到请求后调用 biz 处理
│   ├── clientrpc/  # 有状态 RPC 客户端，通过 Master 的 stream 长连接通信
│   ├── client/     # frpc 生命周期管理（配置/启停）
│   ├── server/     # frps 生命周期管理
│   ├── tunnel/     # 隧道管理（frpc+frps 编排）
│   ├── dao/        # 数据访问对象，**所有数据库操作经由此处**
│   ├── wg/         # WireGuard 组网、路由规划（含较多单测）
│   ├── workerd/    # 边缘 Worker 配置生成与部署
│   ├── rbac/ watcher/ cache/ mux/ rpc/ master/
├── models/         # GORM 模型与迁移（models/db.go 中 AutoMigrate）
├── middleware/     # JWTAuth / AuthCtx / RBAC 等 gin 中间件
├── common/         # 请求/响应封装、context、result（统一 OK/Err 响应）
├── defs/           # 常量、错误码、RBAC 常量、RPC/WG 类型
├── conf/           # 配置（conf/settings.go 有详细注释；读取当前目录 .env）
├── idl/            # protobuf 定义（api_*.proto、rpc_master.proto、types_wg.proto…）
├── pb/             # protoc 生成的 Go 代码（勿手改）
├── middleware/ utils/（含 utils/logger）
├── www/            # Next.js 前端（含 lib/pb 生成的 TS protobuf）
├── docs/           # VitePress 文档（贡献指南见 docs/contribute.md）
├── build.sh        # 交叉编译构建
├── codegen.sh      # proto → Go / TS 代码生成
├── Dockerfile / Dockerfile.standalone / .ko*.yaml / .drone.yaml
```

## 3. 环境准备与常用命令

```bash
# 环境变量（读取当前目录 .env）
cp .env.sample .env        # APP_GLOBAL_SECRET、MASTER_RPC_HOST=127.0.0.1

# 本地调试启动（见 docs/contribute.md）
go run ./cmd/frpp master
go run ./cmd/frpp client -i <clientID> -s <clientSecret>   # 参数复制自 master webui
go run ./cmd/frpp server -i <serverID> -s <serverSecret>

# 构建（前端 + 后端，输出 dist/）
bash ./build.sh                  # 前端部分执行 pnpm install && pnpm build，可 --skip-frontend
bash ./build.sh --skip-frontend  # 仅构建 Go 二进制（交叉编译 amd64/arm64）

# 前端（www/）
cd www && pnpm install && pnpm dev     # 开发
cd www && pnpm run lint                # ESLint（CI 必跑）
cd www && pnpm build                   # 构建后自动复制到 cmd/frpp/out，由 Go embed 嵌入

# 测试（CI 中默认被注释，本地按需运行）
go test ./...

# 模块整理（CI 必跑）
go mod tidy
```

### 代码生成（修改 proto 后必须执行）

```bash
# 依赖：protoc、protoc-gen-go、protoc-gen-go-grpc、@protobuf-ts/plugin
bash ./codegen.sh
```

- Go 侧：`idl/*.proto` → `pb/`
- 前端侧：`idl/*.proto` → `www/lib/pb/*.ts`
- **规则：不要手改 `pb/` 与 `www/lib/pb/` 下的生成文件**，一律改 `idl/` 后重新生成。

## 4. 架构与请求链路

1. 前端 → Master HTTP API（gin，[biz/master/handler.go](biz/master/handler.go) 注册路由）
2. 中间件链：`middleware.JWTAuth` → `AuthCtx` → `RBAC`（用户信息注入 context，可通过 common 包获取）
3. handler 由 `app.Wrapper` 包装，调用 biz 逻辑
4. Master ↔ Client/Server 走 gRPC stream（`services/master` 服务端、`services/clientrpc` 客户端）；Client/Server 侧收到消息后同样经 `biz/` 处理
5. 数据库访问统一走 `services/dao`；定时任务在 `watcher`（如每 30 秒拉取/更新配置）

依赖注入：全项目使用 **go.uber.org/fx**。三种角色分别在 [cmd/frpp/shared/modules.go](cmd/frpp/shared/modules.go) 中通过 `clientMod / serverMod / masterMod / commonMod` 装配依赖，新增服务时应通过 fx 模块提供，而不是手动全局初始化。

## 5. 代码规范与惯例

### 新增 HTTP API 的标准姿势

1. 在 `idl/` 对应 proto 中定义 Request/Response 消息 → 运行 `codegen.sh`
2. 在 `biz/master/<模块>/` 编写 handler，签名统一为：

   ```go
   func MyHandler(ctx *app.Context, req *pb.MyRequest) (*pb.MyResponse, error)
   ```

   通过 `app.Wrapper(appInstance, MyHandler)` 自动完成：解析请求（JSON 或 protobuf）→ 调用 → 统一响应（[services/app/helper.go](services/app/helper.go)）。
3. 在 [biz/master/handler.go](biz/master/handler.go) 的路由组内注册：
   `xxxRouter.POST("/action", app.Wrapper(appInstance, xxx.MyHandler))`
4. 如需 RPC 事件，在 `rpc_master.proto` 增加事件定义，并在 `services/` 与 `biz/client|server` 两侧对接。
5. 如涉及权限，在 `defs/rbac_consts.go` 与 `services/rbac` 补充规则。

### 响应规范

- 成功：`common.OKResp(c, resp)`；失败：`common.ErrResp(c, resp, err)` / `common.ErrUnAuthorized`
- 支持 `application/x-protobuf` 与 JSON 双格式，自动附带 TraceID header（`defs.TraceIDKey`）
- 响应类型需加入 `common/response.go` 的 `RespType` 联合类型（**新增 pb 响应类型时别忘了这一步**）

### 日志规范

- 使用 `utils/logger`（基于 logrus），统一通过带 context 的方式调用：

  ```go
  logger.Logger(ctx).Infof("server %s config changed", id)
  logger.Logger(ctx).WithError(err).Error("cannot load config")
  ```

- 避免裸 `fmt.Println` / `log`；错误日志用 `.WithError(err).Error(...)`

### 数据库

- 模型定义在 `models/`，迁移在 `models/db.go` 中通过 `db.AutoMigrate(...)` 自动完成（新表需加入该列表）
- 所有读写必须经过 `services/dao`，不要在 biz 里直接操作 gorm
- 支持 sqlite3（默认，`glebarez/sqlite`，无需 CGO）、mysql、postgres；配置项 `DB.TYPE` / `DB.DSN`（见 [conf/settings.go](conf/settings.go)）

### 配置

- 所有配置项集中在 `conf/settings.go`，使用 cleanenv + godotenv 读取 `.env`
- 新增配置项需在该文件中声明并附注释，同时考虑更新 `docs/all-configs.md`

## 6. 测试

- 现有测试集中在：`conf/`、`models/`、`services/wg/`、`services/workerd/`、`utils/`
- 运行：`go test ./...`（CI 的 Go 测试目前被注释，但提交前应本地跑通）
- 新增逻辑优先写表驱动单测（testify 已引入）；WireGuard 路由、workerd 配置生成是既有测试范例

## 7. CI/CD 与提交前检查

- GitHub Actions（`.github/workflows/master-merge-check.yml`）在 PR/push main 时执行：
  1. `cd www && pnpm run lint`
  2. `go mod tidy`（检查是否有 diff）
  3. `bash ./build.sh`（需要 protoc 与插件）
  4. `ko build ./cmd/frpp`（含 workerd 变体的 dry-run）
- 发布：`.drone.yaml` 负责 docker buildx 多架构镜像；tag workflow、wiki workflow 亦在 `.github/workflows/`
- **提交前自检清单**：
  - [ ] 修改 proto 后已运行 `codegen.sh`，生成文件已提交
  - [ ] `go build ./...` 通过
  - [ ] `go test ./...` 通过
  - [ ] `cd www && pnpm run lint` 通过
  - [ ] `go mod tidy` 无 diff
  - [ ] 新增 pb 响应类型已加入 `common/response.go` 的 `RespType`
  - [ ] 新增模型已加入 `models/db.go` 的 `AutoMigrate`

## 8. 注意事项（易踩坑）

- `pb/`、`www/lib/pb/`、`cmd/frpp/out`（前端产物）均为生成物，不要手编
- Client/Server 无公网 IP，通信依赖 Master 的 stream 长连接，改动 RPC 时注意两侧（`services/master` 与 `services/clientrpc`、`biz/client`/`biz/server`）同步
- WireGuard 组网目前仅支持 Linux，且要求至少一个 Client 有公网 IP 作为中继；相关代码在 `services/wg`，处于测试阶段
- 默认数据库为 SQLite，个人部署无需独立数据库服务
- 文档在 `docs/`（Vitepress），涉及配置/部署变更时同步更新对应文档页
- 详细架构调用图见 `docs/public/images/callis.svg`，配置项说明见 `docs/all-configs.md`
