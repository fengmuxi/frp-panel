# CI/CD 与发布流程

本项目的自动化脚本位于仓库的 `.github/workflows/` 目录，基于 **GitHub Actions** 实现持续集成与发布。共包含 5 个工作流和 1 个 Issue 模板。

## 工作流总览

| 工作流 | 文件 | 触发时机 | 作用 |
| --- | --- | --- | --- |
| Merge Check | `master-merge-check.yml` | push / PR 到 `main` | 合并前质量检查，不发布 |
| Master Release | `master.workflow.yml` | push / PR 到 `main`、手动 | 发布 `alpha` 预发布版本 |
| Version Tag Release | `tag.workflow.yml` | 推送 `v*` 标签、手动 | 发布正式版本（二进制 + 镜像） |
| workerd Docker | `workerd-docker-tag.workflow.yml` | 推送 `v*` 标签、手动 | 构建带 workerd 的 Docker 镜像 |
| Wiki Deploy | `wiki.workflow.yml` | push 到 `main`、手动 | 构建并部署 VitePress 文档站 |

## 各工作流详细说明

### 1. Merge Check（合并检查）

**触发**：向 `main` 分支 push 或发起 Pull Request。

**流程**：

1. 前端 ESLint 检查（`pnpm run lint`）
2. `go mod tidy`、安装 protoc 及插件
3. 执行 `build.sh` 全量编译，上传 `dist/` 和 `www/out` 构建产物（可在 Actions 运行详情页下载）
4. `ko build --push=false` 干跑构建 Docker 镜像（含普通与 workerd 两种配置），验证镜像可构建但不推送

**使用方法**：无需任何操作，提交 PR 时自动运行。注意当前 Go 单元测试在该工作流中被注释禁用。

### 2. Master Release（Alpha 预发布）

**触发**：push 到 `main`、PR、或在 Actions 页面手动触发（workflow_dispatch）。

**流程**：

1. `build.sh` 编译全平台二进制
2. 创建/更新 **`alpha`** 预发布 Release，附带 `dist/*` 二进制文件
3. 使用 `ko` 构建镜像并推送到 Docker Hub：`docker.io/fengmuxi/frp-panel:alpha`

**使用方法**：代码合入 `main` 后自动执行。手动触发方式：GitHub 仓库 → **Actions** → 选择 **Master Release** → **Run workflow**。

### 3. Version Tag Release（正式版发布）

**触发**：推送 `v*` 格式的 Git 标签（如 `v1.0.0`），或手动触发。

**流程**：

1. `build.sh` 编译全平台二进制
2. 创建两个 Release：**`vX.Y.Z`**（正式版）和 **`latest`**，均附带二进制文件
3. 推送 Docker 镜像标签：`:vX.Y.Z` 和 `:latest`

**使用方法**（正式发版的标准方式）：

```bash
git tag v1.0.0
git push origin v1.0.0
```

推送标签后两个标签触发的工作流（本工作流 + workerd 工作流）会并行执行。

### 4. workerd Docker（workerd 变体镜像）

**触发**：与正式版发布相同，由 `v*` 标签触发。

**与正式版的差异**：

- 使用 `.ko.workerd.yaml` 替代 `.ko.yaml`，基础镜像为 `jacoblincool/workerd`（内置 workerd 运行时）
- `build.sh --current`：仅为当前 runner 平台构建
- 只推送镜像，不创建 Release：`docker.io/fengmuxi/frp-panel:vX.Y.Z-workerd` 和 `:latest-workerd`

**使用方法**：打 `v*` 标签后自动执行。如需使用边缘 Worker（函数）功能，请拉取该变体镜像：

```bash
docker pull docker.io/fengmuxi/frp-panel:latest-workerd
```

> 普通镜像适用于仅做 frp 内网穿透的场景；需要开箱运行边缘 Worker 函数功能时，请使用 `-workerd` 后缀镜像，无需运行时额外下载 workerd 二进制。

### 5. Wiki Deploy（文档站部署）

**触发**：push 到 `main`，或手动触发。

**流程**：在 `docs/` 目录用 pnpm 构建 VitePress 站点，并部署到 **GitHub Pages**。

**使用方法**：文档修改合入 `main` 后自动更新。首次使用需在仓库 **Settings → Pages → Build and deployment → Source** 中选择 **GitHub Actions**。

## 需要配置的仓库变量（Secrets）

在仓库 **Settings → Secrets and variables → Actions → Secrets** 中添加：

| 变量名 | 必需性 | 用途 |
| --- | --- | --- |
| `DOCKERHUB_USERNAME` | 必需 | Docker Hub 登录用户名，用于推送 `docker.io/fengmuxi/frp-panel` 镜像 |
| `DOCKERHUB_TOKEN` | 必需 | Docker Hub 访问令牌（Access Token），在 Docker Hub → Account Settings → Personal access tokens 生成 |
| `GITHUB_TOKEN` | 无需配置 | GitHub 自动提供，用于创建 Release，工作流中已自动引用 |

**未配置 Docker Hub Secrets 的影响**：

- **Merge Check** 和 **Wiki Deploy** 不受影响，可正常运行
- **Master Release** / **Version Tag Release** 的 Release 二进制上传步骤正常，但在推送 Docker 镜像步骤失败
- **workerd Docker** 工作流整体失败（它只做镜像推送）

## 本地等价构建

工作流中的编译步骤可通过 `build.sh` 在本地复现：

```bash
bash ./build.sh                                    # 完整构建（前端 + 全平台二进制）
bash ./build.sh --platform linux --arch amd64      # 仅构建指定平台
bash ./build.sh --bintype full                     # 仅构建完整版（排除 client）
bash ./build.sh --current                          # 仅构建当前系统（workerd 工作流同款参数）
bash ./build.sh --skip-frontend                    # 跳过前端构建
```

输出文件位于 `dist/` 目录。

## Issue 模板

`.github/ISSUE_TEMPLATE/问题反馈-bug-report.md` 为 Bug 反馈模板，用户在 GitHub 点击 **New Issue** 时自动加载，无需运行。
