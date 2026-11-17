# 函数功能（边缘 Worker）使用指南

frp-panel 的**函数**功能基于 [Cloudflare workerd](https://github.com/cloudflare/workerd) 运行时，允许你在任意 Client 节点上部署 JavaScript 边缘函数，并通过 frp 隧道从公网 Server 暴露为 HTTP 服务，实现轻量的边缘计算、请求改写、动态代理等能力。

## 工作原理

```
访问者 ──HTTP──▶ Server(frps) ──frp 隧道──▶ Client 节点
                                            │  frp UnixDomainSocket 插件
                                            ▼
                              workerd（abstract unix socket）
                                            │
                                            ▼
                                     你的 JS 代码 (fetch handler)
```

- workerd 在节点上监听一个 abstract unix socket（`/tmp/frpp-worker-<id>.sock`）
- 每个"入口"对应一条 HTTP 类型的 frp 代理，把 Server 上 `<WorkerID>.<子域名后缀>`（或自定义域名）的请求转发到该 socket
- 同一 Worker 可部署到多个 Client，入口按负载均衡组分发

## 前置条件

### 1. 功能开关

函数功能默认**开启**（`CLIENT_FEATURES_ENABLE_FUNCTIONS=true`）。如需在某个节点关闭，参见 [FAQ](./faq.md)。

### 2. 节点安装 workerd 运行时（三选一）

| 方式 | 适用场景 | 说明 |
| --- | --- | --- |
| **`-workerd` Docker 镜像**（推荐） | Docker 部署的节点 | `docker.io/fengmuxi/frp-panel:latest-workerd` 基础镜像内置 workerd，开箱即用，无需下载 |
| **面板一键安装** | Linux amd64/arm64 节点 | 编辑页"部署"卡片中点击下载按钮，从 Cloudflare 官方 GitHub Release 下载（自动解压、SHA-256 指纹校验） |
| **手动指定二进制** | Windows / macOS 或离线节点 | 在 Client 的 `.env` 中设置 `CLIENT_WORKER_WORKERD_BINARY_PATH` 指向本地 workerd 可执行文件 |

一键安装相关配置（Client `.env`）：

```bash
# 下载地址必须为 https，默认取 Cloudflare 官方 Release
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_LINUX_X86_64=https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-64.gz
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_LINUX_ARM64=https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-arm64.gz
# 是否拼接 GitHub 加速前缀（默认 true）
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_USE_PROXY=true
# workerd 工作目录（默认 /tmp/frpp/workerd）
CLIENT_WORKER_WORKERD_WORK_DIR=/tmp/frpp/workerd
```

> 安全说明：面板安装 workerd 时忽略请求中的自定义下载地址，强制使用节点本地配置的 HTTPS 地址，并对下载产物做 SHA-256 指纹锁定（首次安装记录，后续不一致则拒绝安装）。

### 3. Server 配置子域名后缀

入口默认使用 `subdomain = WorkerID` 访问，因此需要在 **Server 配置**中填写 `subDomainHost`（如 `example.com`，并将其 DNS 泛解析到 Server），访问地址即为 `<WorkerID>.example.com`。也可以在创建入口后通过"编辑"改用自定义域名。

## 操作步骤

### 第一步：创建 Worker

1. 左侧菜单点击 **函数**（`/workers`）
2. 点击 **创建** 按钮，填写：
   - **客户端ID**：选择初始部署的 Client 节点
   - **Worker名称**：函数名称
3. 点击 **提交**，创建成功后进入列表

### 第二步：编辑页概览

在列表中点击 **编辑** 进入编辑页（`/worker-edit`），页面分为三个标签：

| 标签 | 内容 |
| --- | --- |
| **信息** | 基本信息（名称/描述）、**部署**卡片、**入口**卡片 |
| **代码** | JavaScript 函数代码编辑器（入口文件 `entry.js`） |
| **模板** | workerd 配置模板（capnp 格式，一般无需修改） |

修改任何内容后，点击右上角 **保存** 生效。

### 第三步：部署与安装 workerd

在"信息"标签的 **部署** 卡片中：

1. 点击 **添加部署**，选择目标 Client 节点
2. 若该节点尚未安装 workerd，点击部署卡片中的 **下载** 按钮 → **安装**（仅 Linux amd64/arm64；其他平台请使用 `-workerd` 镜像或手动指定二进制）
3. 每个部署的 Client 会显示实时状态徽章：

| 状态 | 含义 |
| --- | --- |
| 运行中 | workerd 进程正常运行 |
| 已停止 | 进程未运行 |
| 错误 | 启动或运行出错 |
| 未知 | 尚未获取到状态 |

### 第四步：编写函数代码

切换到 **代码** 标签，默认模板如下：

```javascript
export default {
  async fetch(req, env) {
    try {
      let resp = new Response("worker: " + req.url + " is online! -- " + new Date())
      return resp
    } catch(e) {
      return new Response(e.stack, { status: 500 })
    }
  }
};
```

- 代码遵循 workerd / Cloudflare Workers 的 `fetch` 处理器规范
- 点击 **保存** 后，Master 会自动将新代码下发到所有部署的 Client，workerd 以 `--watch` 模式运行，文件更新后自动重载，无需手动重启

### 第五步：创建入口（对外暴露）

在"信息"标签的 **入口** 卡片中：

1. 点击 **创建**，选择：
   - **选择客户端**：Worker 所部署的 Client（流量落地端）
   - **选择服务器**：提供公网访问的 Server
2. 提交后生成一条 HTTP 类型的 frp 代理（名称形如 `ingress-xxxx-<clientID>`），入口卡片会显示：
   - **Server**：从访问者视角的访问地址预览（含状态徽章）
   - **Client**：流量落地的节点
3. 可对入口执行：
   - **编辑**：修改 HTTP 代理配置（子域名、自定义域名等）
   - **删除**：移除该入口（不会删除 Worker 本体）

### 第六步：访问验证

- 等待部署卡片状态变为**运行中**、入口状态为**运行中**
- 通过入口卡片上的 Server 访问预览地址发起请求（如 `<WorkerID>.example.com`）
- 使用默认代码时，返回 `worker: <请求URL> is online! -- <时间>` 即表示链路打通

## 管理操作（列表页）

函数列表页展示 **Worker名称 / 状态 / WorkerID**，行内 **操作** 菜单：

- **编辑**：进入编辑页
- **重启**：重新下发 Worker（进程异常时使用）
- **删除**：删除 Worker 及其部署关系

## 日志查看

workerd 进程的 stdout/stderr 会汇入所在 Client 节点的日志（日志中带 `workerd` 标签）。可通过面板的**实时日志**（Stream Log）页面选择对应 Client 查看。

## 高级：配置模板

**模板** 标签编辑的是生成 `workerd.capnp` 所用的 Go template，默认模板关键字段：

```
address = "{{.Socket.Address}}"     # unix-abstract:/tmp/frpp-worker-<id>.sock
service = "{{.WorkerId}}"
```

可用变量：`WorkerId`、`UserId`、`CodeEntry`（入口文件名，默认 `entry.js`）、`Socket.Address`。仅在需要调整 workerd 监听行为时修改，修改后同样点击**保存**生效。

## 常见问题

- **创建入口后访问 404/不通？**
  1. 检查 Server 是否配置了 `subDomainHost` 且 DNS 已泛解析
  2. 检查部署卡片状态是否为"运行中"（未安装 workerd 会一直是未知/错误）
  3. 检查入口状态，以及入口的 Server 访问预览地址是否正确
- **一键安装 workerd 失败？** 非 Linux 节点不支持一键安装，请改用 `-workerd` Docker 镜像或设置 `CLIENT_WORKER_WORKERD_BINARY_PATH`；Linux 节点请确认网络可访问 GitHub（或配置代理）。
- **如何彻底关闭某节点的函数功能？** 见 [FAQ](./faq.md)。
