# Functions (Edge Worker) Guide

The **Functions** feature of frp-panel runs on the [Cloudflare workerd](https://github.com/cloudflare/workerd) runtime. It lets you deploy JavaScript edge functions on any Client node and expose them as HTTP services through frp tunnels from a public Server — enabling lightweight edge computing, request rewriting, dynamic proxying, and more.

## How It Works

```
Visitor ──HTTP──▶ Server (frps) ──frp tunnel──▶ Client node
                                               │  frp UnixDomainSocket plugin
                                               ▼
                                 workerd (abstract unix socket)
                                               │
                                               ▼
                                     Your JS code (fetch handler)
```

- workerd listens on an abstract unix socket on the node (`/tmp/frpp-worker-<id>.sock`)
- Each **Ingress** is an HTTP-type frp proxy that forwards requests for `<WorkerID>.<subdomain suffix>` (or a custom domain) on the Server to that socket
- A Worker can be deployed to multiple Clients; ingress requests are distributed across them via a load-balancing group

## Prerequisites

### 1. Feature Switch

The functions feature is **enabled by default** (`CLIENT_FEATURES_ENABLE_FUNCTIONS=true`). To disable it on a specific node, see the [FAQ](./faq.md).

### 2. Install the workerd Runtime on the Node (choose one)

| Method | Suitable for | Notes |
| --- | --- | --- |
| **`-workerd` Docker image** (recommended) | Docker-deployed nodes | `docker.io/fengmuxi/frp-panel:latest-workerd` ships with workerd built in — works out of the box, no download needed |
| **One-click install in the panel** | Linux amd64/arm64 nodes | Click the download button in the "Deploy" card of the edit page; downloads from the official Cloudflare GitHub Release (auto-extracted, SHA-256 fingerprinted) |
| **Manual binary path** | Windows / macOS or offline nodes | Set `CLIENT_WORKER_WORKERD_BINARY_PATH` in the Client's `.env` to point to a local workerd executable |

One-click install configuration (Client `.env`):

```bash
# Download URL must be https; defaults to the official Cloudflare Release
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_LINUX_X86_64=https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-64.gz
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_LINUX_ARM64=https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-arm64.gz
# Whether to prepend the GitHub acceleration prefix (default true)
CLIENT_WORKER_WORKERD_DOWNLOAD_URL_USE_PROXY=true
# workerd working directory (default /tmp/frpp/workerd)
CLIENT_WORKER_WORKERD_WORK_DIR=/tmp/frpp/workerd
```

> Security note: when installing workerd, the panel ignores custom download URLs from the request, enforces the node-local HTTPS configuration, and pins the SHA-256 checksum of the downloaded artifact (recorded on first install; later installs are rejected on mismatch).

### 3. Configure the Subdomain Suffix on the Server

Ingresses are accessed via `subdomain = WorkerID` by default, so configure `subDomainHost` (e.g. `example.com`, with wildcard DNS pointing to the Server) in the **Server configuration**. The access URL becomes `<WorkerID>.example.com`. Alternatively, edit an ingress after creation to use a custom domain.

## Step-by-Step Usage

### Step 1: Create a Worker

1. Click **Functions** (`/workers`) in the left sidebar
2. Click **Create** and fill in:
   - **Client ID**: the Client node for initial deployment
   - **Worker Name**: name of the function
3. Click **Submit**; you will be returned to the list

### Step 2: Edit Page Overview

Click **Edit** in the list to open the edit page (`/worker-edit`), which has three tabs:

| Tab | Contents |
| --- | --- |
| **Info** | Basic info (name/description), **Deploy** card, **Ingress** card |
| **Code** | JavaScript function code editor (entry file `entry.js`) |
| **Template** | workerd configuration template (capnp format; rarely needs changes) |

After modifying anything, click **Save** in the top-right corner to apply.

### Step 3: Deploy and Install workerd

In the **Deploy** card on the **Info** tab:

1. Click **Add Deploy** and choose the target Client node
2. If the node does not have workerd yet, click the **download** button on the deploy card → **Install** (Linux amd64/arm64 only; for other platforms use the `-workerd` image or a manual binary path)
3. Each deployed Client shows a live status badge:

| Status | Meaning |
| --- | --- |
| Running | workerd process is running normally |
| Stopped | Process is not running |
| Error | Startup or runtime error |
| Unknown | Status not yet reported |

### Step 4: Write Function Code

Switch to the **Code** tab; the default template is:

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

- Code follows the workerd / Cloudflare Workers `fetch` handler specification
- After clicking **Save**, the Master pushes the new code to all deployed Clients; workerd runs with `--watch`, so files are hot-reloaded without a manual restart

### Step 5: Create an Ingress (Expose Publicly)

In the **Ingress** card on the **Info** tab:

1. Click **Create** and choose:
   - **Select Client**: the Client where the Worker is deployed (traffic landing end)
   - **Select Server**: the Server providing public access
2. After submission, an HTTP-type frp proxy is created (named like `ingress-xxxx-<clientID>`). The ingress card shows:
   - **Server**: the visitor-side access URL preview (with a status badge)
   - **Client**: the traffic landing node
3. Each ingress supports:
   - **Edit**: modify the HTTP proxy configuration (subdomain, custom domains, etc.)
   - **Delete**: remove the ingress (the Worker itself is kept)

### Step 6: Verify Access

- Wait until the deploy card shows **Running** and the ingress status is **Running**
- Request the Server access URL shown on the ingress card (e.g. `<WorkerID>.example.com`)
- With the default code, a response of `worker: <request URL> is online! -- <time>` means the whole chain works

## Management (List Page)

The Functions list shows **Worker Name / Status / WorkerID**, with an **Actions** menu per row:

- **Edit**: open the edit page
- **Restart**: re-dispatch the Worker (use when the process is abnormal)
- **Delete**: remove the Worker and its deployment relations

## Viewing Logs

workerd's stdout/stderr is merged into the hosting Client node's logs (tagged with `workerd`). Use the panel's **Stream Log** page and select the corresponding Client.

## Advanced: Configuration Template

The **Template** tab edits the Go template used to generate `workerd.capnp`. Key fields of the default template:

```
address = "{{.Socket.Address}}"     # unix-abstract:/tmp/frpp-worker-<id>.sock
service = "{{.WorkerId}}"
```

Available variables: `WorkerId`, `UserId`, `CodeEntry` (entry file name, default `entry.js`), `Socket.Address`. Only modify this when you need to adjust workerd's listening behavior; changes apply on **Save** as well.

## FAQ

- **Access returns 404 / no response after creating an ingress?**
  1. Check that the Server has `subDomainHost` configured and wildcard DNS is in place
  2. Check that the deploy card status is "Running" (without workerd installed it stays Unknown/Error)
  3. Check the ingress status and the access URL preview on the ingress card
- **One-click workerd install fails?** Non-Linux nodes are not supported — use the `-workerd` Docker image or set `CLIENT_WORKER_WORKERD_BINARY_PATH`; on Linux nodes make sure GitHub is reachable (or configure a proxy).
- **How to fully disable the functions feature on a node?** See the [FAQ](./faq.md).
