# Master 部署

Master 推荐使用 docker 部署！不推荐直接安装到服务器中

会给出三种部署方式，任选一种即可

部署后没有默认用户，注册的第一个用户即为管理员，为了安全，默认不开启多用户注册

程序的默认存储数据路径和程序文件同目录，如需修改请参考配置表格

重要！：如果你想只部署 master，同时作为 server 运行，请不要忘记启动 master 后，在 webui 的配置服务端中修改 default 的配置

## 前期准备

### 服务器开放公网端口：

- **WEBUI 端口**: 默认 `TCP 9000`
- **RPC 端口**: 默认 `TCP 9001`
- **frps 的API端口**：没有默认，请随意预留，例子使用 `TCP/UDP 7000`
- **frps 对外开放的服务端口**：没有默认，请随意预留，例子使用 `TCP/UDP 26999-27050`

如果使用反向代理，请忽略 WEBUI 和 RPC 端口，放行 80/443 即可；
此时建议配合 `MASTER_API_LISTEN_IP` / `MASTER_RPC_LISTEN_IP` 把这两个监听收紧到 `127.0.0.1`（默认 `0.0.0.0` 监听所有网卡），详见下方部署示例

WEBUI 端口也可以处理 h2c 格式的 RPC 连接

RPC 端口也可以处理自签名 HTTPS 的 API 连接

二者都可使用反向代理服务器连接并提供TLS

如果你想要使用安全的方式（反向代理），请参考下图设置环境变量「`CLIENT_RPC_URL`和`CLIENT_API_URL`」。

注意⚠️：请首先使用普通部署的方式部署成功！然后再来调整这两个变量！！！！

橙色是不安全，绿色是安全。你需要保证两个环境变量都有设置，才能正常工作

![](./public/images/frp-panel-platform-connection-env.svg)

> 测试端口是否开放的方法（以8080为例），在服务器上运行：
> ```shell
> python3 -m http.server 8080
> ```
> 然后在另一台电脑/服务器上执行：
> ```shell
> curl http://服务器公网IP/域名:8080 -I
> ```
> 成功的话，输出类似
> ```
> HTTP/1.0 200 OK
> Server: SimpleHTTP/0.6 Python/3.11.0
> Date: Sat, 12 Apr 2025 17:12:15 GMT
> Content-type: text/html; charset=utf-8
> Content-Length: 8225
> ```

## 必填与关键环境变量

Docker 与裸机部署共用同一套变量，完整清单见 [配置说明](./all-configs.md)。
**`APP_` / `MASTER_` 前缀不可省略**——程序按 `env-prefix` 拼接读取，漏写前缀的变量会被静默忽略（不会报错，但也不生效）。

| 变量 | 是否必填 | 默认值 | 说明 |
|------|---------|--------|------|
| `APP_GLOBAL_SECRET` | **必填** | `frp-panel`（禁用） | >=16 位强随机串，生成 `openssl rand -hex 32`。为空、过短或仍是默认值时 master 拒绝启动。它派生 JWT 签名密钥与节点 secret，**库内已有密文后更换必须走 `frpp migrate rekey`，否则节点掉线** |
| `MASTER_RPC_HOST` | 节点接入必填 | `127.0.0.1` | 节点可访问的公网 IP/域名，默认回环地址远程节点连不上 |
| `MASTER_API_HOST` | 反代/多域名必填 | 同 `MASTER_RPC_HOST` | 面板对外地址 |
| `CLIENT_RPC_URL` 与 `CLIENT_API_URL` | 反代必填 | 由 `MASTER_*` 自动拼接 | 节点视角访问 master 的地址，**两个都要设** |
| `APP_COOKIE_SECURE` | 纯 HTTP 必填 | `true` | 无 TLS 反代时必须显式设为 `false`，否则浏览器不回传 Cookie，登录态异常 |
| `APP_ENABLE_REGISTER` | 否 | `false` | 首个管理员注册不受此开关限制；要继续开放注册才设 `true` |
| `DB_TYPE` / `DB_DSN` | 否 | `sqlite3` / `/data/data.db?_pragma=journal_mode(WAL)` | 镜像已内置默认值，改库位置只需覆盖 `DB_DSN`（注意用 **`DB_DSN`，不带 `APP_` 前缀**） |

## 在 Linux 上部署

### 方式一：Docker Compose 部署

服务器需要安装docker和docker compose

首先创建一个`docker-compose.yaml`文件，写入以下内容

```yaml
services:
  frpp-master:
    image: fengmuxi/frp-panel:latest
    network_mode: host
    environment:
      # 必填：>=16 位强随机串，生成 `openssl rand -hex 32`，不要泄露。
      # 下面的占位值故意短于 16 位：直接启动会被拒绝（安全兜底），改完才能起
      APP_GLOBAL_SECRET: change-me
      # 历史部署仍使用默认密钥 frp-panel 时，可临时设 false 允许启动，
      # 之后强烈建议 `frpp migrate rekey` 轮换成强随机值再移除本开关
      # APP_REQUIRE_STRONG_SECRET: "false"
      MASTER_RPC_HOST: 1.2.3.4 # 服务器的外部IP或域名
      MASTER_RPC_PORT: 9001 # RPC 监听端口
      MASTER_API_HOST: 1.2.3.4 # 服务器的外部IP或域名
      MASTER_API_PORT: 9000 # API/WebUI监听端口
      # 监听 IP：默认 0.0.0.0 = 监听服务器所有网卡（含公网）。
      # 若面板只经本机反代访问、或节点全部走域名接入，建议改 127.0.0.1 收紧暴露面：
      # MASTER_API_LISTEN_IP: 127.0.0.1   # API/WebUI 绑定地址（配合 80/443 反代）
      # MASTER_RPC_LISTEN_IP: 127.0.0.1   # 节点 RPC 绑定地址（节点全走反代时才收紧）
      # 下方 http:// 示例为纯 HTTP 部署，Cookie Secure 必须关掉，否则登录态异常；
      # 走 HTTPS 反代时删掉这行（保持默认 true）
      APP_COOKIE_SECURE: false
      # CLIENT_RPC_URL和CLIENT_API_URL请根据实际情况设置，设置为外部可以通过url访问到master的形式
      # Client 连接 master RPC 的 URL，如果使用反向代理，请设置为通过反向代理访问的 URL（如wss://example.com:443）
      CLIENT_RPC_URL: grpc://1.2.3.4:9001
      # Client 连接 master API/WebUI 的 URL，如果使用反向代理，请设置为通过反向代理访问的 URL（如https://example.com:443）
      CLIENT_API_URL: http://1.2.3.4:9000
      # 需要继续开放注册时才打开；首个管理员注册不受此开关限制
      # APP_ENABLE_REGISTER: "true"
    volumes:
      - ./data:/data # 数据存储位置
    restart: unless-stopped
    command: master
```

### 方式二：Docker 命令部署

服务器需要安装 docker，我们推荐使用 host 网络模式部署 `Master`

```bash
# 推荐
# MASTER_RPC_HOST等0.0.0.0要改成你服务器的外部IP
# APP_GLOBAL_SECRET 必填，>=16 位强随机串（openssl rand -hex 32），注意不要泄漏，
# 客户端和服务端的是通过Master生成的；占位值 change-me 过短，master 会拒绝启动
# CLIENT_RPC_URL和CLIENT_API_URL请根据实际情况设置
# 如果使用反向代理，请设置为通过反向代理访问的 URL，也就是外部如何访问master
# 如 443端口代理example.com到9000端口
# CLIENT_RPC_URL=wss://example.com:443
# CLIENT_API_URL=https://example.com:443
# 走 HTTPS 反代时删除 APP_COOKIE_SECURE 这一行（保持默认 true）
docker run -d \
	--network=host \
	--restart=unless-stopped \
	-v /opt/frp-panel:/data \
	-e APP_GLOBAL_SECRET=change-me \
	-e MASTER_RPC_HOST=0.0.0.0 \
	-e CLIENT_RPC_URL=grpc://0.0.0.0:9001 \
	-e CLIENT_API_URL=http://0.0.0.0:9000 \
	-e APP_COOKIE_SECURE=false \
	fengmuxi/frp-panel
```

如果你不想使用 host 网络模式，请参考使用下面的命令修改

```bash
# 或者
# 运行时记得删除命令中的中文
# CLIENT_RPC_URL和CLIENT_API_URL请根据实际情况设置，设置为外部可以通过url访问到master的形式
# 如果使用反向代理，请设置为通过反向代理访问的 URL，也就是外部如何访问master
# 走 HTTPS 反代时删除 APP_COOKIE_SECURE 这一行（保持默认 true）
docker run -d -p 9000:9000 \ # API控制台端口
	-p 9001:9001 \ # rpc端口
	-p 7000:7000 \ # frps 端口
	-p 27000-27050:27000-27050 \ # 给frps预留的端口
	--restart=unless-stopped \
	-v /opt/frp-panel:/data \ # 数据存储位置
	-e APP_GLOBAL_SECRET=change-me \ # Master的secret（>=16位强随机），注意不要泄漏，客户端和服务端的是通过Master生成的
	-e MASTER_RPC_HOST=0.0.0.0 \ # 这里要改成你服务器的外部IP
	-e CLIENT_RPC_URL=grpc://0.0.0.0:9001 \
	-e CLIENT_API_URL=http://0.0.0.0:9000 \
	-e APP_COOKIE_SECURE=false \
	fengmuxi/frp-panel
```

### 方式三：使用 docker 反向代理 TLS 加密部署

这里我们以 [Traefik](https://traefik.io/traefik/) 为例

> `Traefik` 可以实时自动识别 Docker 容器的端口并热更新配置，非常适合 Docker 服务的反向代理

首先创建一个名为`traefik`的反向代理专用网络
```bash
docker network create traefik
```
然后启动反向代理和 Master 服务
- `docker-compose.yaml`

```yaml
version: '3'

services:
  traefk-reverse-proxy:
    image: traefik:v3.3
    restart: unless-stopped
    networks:
      - traefik
    command:
      - --entryPoints.web.address=:80
      - --entryPoints.websecure.address=:443
      - --entryPoints.websecure.http2.maxConcurrentStreams=250
      - --providers.docker
      - --providers.docker.network=traefik
      - --api.insecure # 在生产环境请删除这一行
    # 这下面使用 80 端口做ACME HTTP DNS证书验证
      - --certificatesresolvers.le.acme.email=me@example.com
      - --certificatesresolvers.le.acme.storage=/etc/traefik/conf/acme.json
      - --certificatesresolvers.le.acme.httpchallenge=true
    ports:
      # 反向代理的 HTTP 端口
      - "80:80"
      # 反向代理的 HTTPS 端口
      - "443:443"
      # Traefik 的 Web UI (--api.insecure=true 会使用这个端口)
      # 生产环境请删除这个端口
      - "8080:8080"
    volumes:
      # 挂载 docker.sock，这样 Traefik 可以自动识别主机上所有 docker 容器反向代理配置
      - /var/run/docker.sock:/var/run/docker.sock
      # 保存 Traefik 申请的证书
      - ./conf:/etc/traefik/conf

  frpp-master:
    image: fengmuxi/frp-panel:latest # 这里换成你想使用的版本
    environment:
      # 必填：>=16 位强随机串（openssl rand -hex 32），占位值过短会被拒绝启动
      APP_GLOBAL_SECRET: change-me
      # 本示例走 HTTPS（MASTER_API_SCHEME: https），APP_COOKIE_SECURE 保持默认 true，无需设置
	# 因为 api 和 rpc 使用的协议不一样
	# 我们需要对 api 和 rpc 使用两个域名
	# 以便反向代理正确识别需要转发的协议
      MASTER_RPC_HOST: frpp.example.com
      MASTER_API_PORT: 443
      MASTER_API_HOST: frpp.example.com
      MASTER_API_SCHEME: https
      CLIENT_RPC_URL: wss://frpp.example.com:443
      CLIENT_API_URL: https://frpp.example.com:443
    networks:
      - traefik
    volumes:
      - ./data:/data
    ports:
	  # 无需为 master 预留 api 和 rpc 端口
	  # 预留frps api端口
      - 7000:7000
      - 7000:7000/udp
	  # 预留frps的业务端口
	  # 26999 端口是留给 frps 的http代理端口
      - 26999-27050:26999-27050
      - 26999-27050:26999-27050/udp
    restart: unless-stopped
    command: master
    labels:
	  # API/WSS
      - traefik.http.routers.frp-panel-api.rule=Host(`frpp.example.com`)
      - traefik.http.routers.frp-panel-api.tls=true
      - traefik.http.routers.frp-panel-api.tls.certresolver=le
      - traefik.http.routers.frp-panel-api.entrypoints=websecure
      - traefik.http.routers.frp-panel-api.service=frp-panel-api
      - traefik.http.services.frp-panel-api.loadbalancer.server.port=9000
      - traefik.http.services.frp-panel-api.loadbalancer.server.scheme=http
      # 下方如果你用不到 frps 的http代理，可以不要
      # 需要配置域名 *.frpp.example.com 泛解析到你服务器的公网IP
      # 这样可以实现使用 .frpp.example.com 结束的域名，在 443 端口，转发多个服务到多个 frpc
      - traefik.http.routers.frp-panel-tunnel.rule=HostRegexp(`.*.frpp.example.com`)
      - traefik.http.routers.frp-panel-tunnel.tls.domains[0].sans=*.frpp.example.com
      - traefik.http.routers.frp-panel-tunnel.tls=true
      - traefik.http.routers.frp-panel-tunnel.tls.certresolver=le
      - traefik.http.routers.frp-panel-tunnel.entrypoints=websecure
      - traefik.http.routers.frp-panel-tunnel.service=frp-panel-tunnel
      - traefik.http.services.frp-panel-tunnel.loadbalancer.server.port=26999
      - traefik.http.services.frp-panel-tunnel.loadbalancer.server.scheme=http
networks:
  traefik:
    external: true
    name: traefik
```

上方的 `docker-compose.yaml` 部署完成后，可以访问 `服务器公网IP/域名:8080` 查看反向代理状态

随后配置 default server 即可实现 frp 子域名转发：

| 配置项 | 值 |
|----|-----|
|	FRPs 监听端口	|	7000	|
|	FRPs 监听地址	|	0.0.0.0	|
|	代理监听地址	|	0.0.0.0	|
| 	HTTP 监听端口	|	26999	|
|	域名后缀		|	frpp.example.com	|

## 在 Windows 上部署

### 直接运行

在下载的可执行文件同名文件夹下创建一个 `.env` 文件(注意不要有后缀名)，然后输入以下内容保存后运行对应命令

```
APP_GLOBAL_SECRET=change-me   # 必填，>=16 位强随机串，生成 `openssl rand -hex 32`
MASTER_RPC_HOST=127.0.0.1
MASTER_API_HOST=127.0.0.1
DB_DSN=data.db
CLIENT_RPC_URL=grpc://IP:9001
CLIENT_API_URL=http://IP:9000
# 纯 HTTP（无 TLS 反代）必填，否则登录态异常；注意 APP_ 前缀不可省略
APP_COOKIE_SECURE=false
# 需要继续开放注册时才设 true（首个管理员注册不受限制）
APP_ENABLE_REGISTER=false
```

> 裸写 `COOKIE_SECURE` / `ENABLE_REGISTER`（不带 `APP_`）不会被读取，属于静默失效。

- master: `frp-panel-amd64.exe master`

## 安全默认值与升级说明（2026-11-15 安全加固）

新版 master 对安全基线做了收紧，**从旧版本升级前请逐条核对**：

### 强制项

1. **APP_GLOBAL_SECRET 必须为强随机值**（>=16 字符，不得为默认值 `frp-panel`），否则 master 拒绝启动。
   生成方式：`openssl rand -hex 32`
2. **首次启动自动执行数据库迁移**（版本表 `schema_migrations`）：
   - 迁移 002：为 WireGuard endpoint 回填租户归属
   - 迁移 003：将节点 `connect_secret` 与 SMTP 密码以 AES-256-GCM 静态加密入库
   - sqlite 库在迁移前自动生成 `<db>.bak-migration-vN-<时间戳>` 快照（滚动保留 5 份）
   - 也可手动执行：`frpp migrate status` / `frpp migrate up` / `frpp migrate verify`
3. **`APP_COOKIE_SECURE` 默认 true**：纯 HTTP（无 TLS 反代）部署会话异常时，需上 HTTPS 或显式设置 `APP_COOKIE_SECURE=false`（`APP_` 前缀不可省略，裸写 `COOKIE_SECURE` 不生效）
4. **CLIENT_TLS_INSECURE_SKIP_VERIFY 默认 false**：节点侧自签证书需要将 master CA 纳入信任，或显式回退为 true
5. **server API（8999）默认仅监听 127.0.0.1**：本机 frps 认证插件回源使用；如需外部访问自行改监听并加防火墙

### 换 GLOBAL_SECRET（敏感字段密钥轮换）

`connect_secret` 等敏感字段用 `APP_GLOBAL_SECRET` 派生密钥加密。**库中已有密文后直接换 GLOBAL_SECRET 会导致节点掉线**，正确流程：

```bash
# 1. 停止 master
# 2. 用新 GLOBAL_SECRET 启动一次（.env 中改为新值），让本次运行的 frpp 携带新钥
# 3. 执行轮换（--old-secret 为换钥前的旧值）：
frpp migrate rekey --old-secret=<旧值>
# 4. 用新 GLOBAL_SECRET 正常启动 master；所有 web 会话需重新登录，节点无需操作
```

`rekey` 幂等可重复：中断后直接重跑即可；sqlite 自动先备份（`<db>.bak-rekey-<时间戳>`）。

### 证书轮换

面板自签 CA 有效期为 **3 年**。到期或私钥疑似泄露时：在 webui 删除 default 证书后重启 master 会自动重新签发；
节点侧需更新信任的 CA（`frpp check` 的 `data/cert_expiry` 项会提前告警）。

### 节点接入建议

join token 走命令行参数 `-j` 会留在 shell 历史中，推荐：

```bash
echo <token> > /etc/frpp/token   # chmod 600
frpp join --token-file /etc/frpp/token -r <rpc-url>
```

### 安全巡检

`frpp check` 内置 78 项检查（含安全组）：限流生效性（`auth/rate_limit`）、TLS 校验（`env/tls_verify`）、
敏感字段加密完成度（`data/secret_encryption`）、endpoint 租户归属（`data/endpoint_tenant_scope`）等，
升级后建议跑一次 `frpp check --group auth --group env --group data` 确认基线。
