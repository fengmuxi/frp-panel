# CI/CD and Release Process

This project's automation scripts live in `.github/workflows/` and are built on **GitHub Actions** for continuous integration and release. There are 5 workflows and 1 issue template in total.

## Workflow Overview

| Workflow | File | Trigger | Purpose |
| --- | --- | --- | --- |
| Merge Check | `master-merge-check.yml` | push / PR to `main` | Pre-merge quality checks, no release |
| Master Release | `master.workflow.yml` | push / PR to `main`, manual | Publish `alpha` prerelease |
| Version Tag Release | `tag.workflow.yml` | push of `v*` tags, manual | Publish formal release (binaries + images) |
| workerd Docker | `workerd-docker-tag.workflow.yml` | push of `v*` tags, manual | Build Docker image with workerd included |
| Wiki Deploy | `wiki.workflow.yml` | push to `main`, manual | Build and deploy the VitePress docs site |

## Workflow Details

### 1. Merge Check

**Trigger**: push to `main` or opening a Pull Request.

**Steps**:

1. Frontend ESLint (`pnpm run lint`)
2. `go mod tidy`, install protoc and plugins
3. Run `build.sh` for a full build, upload `dist/` and `www/out` artifacts (downloadable from the Actions run details page)
4. `ko build --push=false` dry-run of the Docker image (both normal and workerd configs) to verify the image builds, without pushing

**Usage**: no action needed; it runs automatically on PRs. Note that Go unit tests are currently commented out in this workflow.

### 2. Master Release (Alpha Prerelease)

**Trigger**: push to `main`, PR, or manual dispatch from the Actions tab.

**Steps**:

1. `build.sh` compiles binaries for all platforms
2. Creates/updates the **`alpha`** prerelease with `dist/*` binaries attached
3. Uses `ko` to build and push the image to Docker Hub: `docker.io/fengmuxi/frp-panel:alpha`

**Usage**: runs automatically after merging into `main`. To trigger manually: repository → **Actions** → **Master Release** → **Run workflow**.

### 3. Version Tag Release (Formal Release)

**Trigger**: pushing a `v*` Git tag (e.g. `v1.0.0`), or manual dispatch.

**Steps**:

1. `build.sh` compiles binaries for all platforms
2. Creates two releases: **`vX.Y.Z`** (formal) and **`latest`**, both with binaries attached
3. Pushes Docker image tags `:vX.Y.Z` and `:latest`

**Usage** (the standard way to release):

```bash
git tag v1.0.0
git push origin v1.0.0
```

Once the tag is pushed, both tag-triggered workflows (this one and the workerd workflow) run in parallel.

### 4. workerd Docker (workerd Variant Image)

**Trigger**: same as the formal release, via `v*` tags.

**Differences from the formal release**:

- Uses `.ko.workerd.yaml` instead of `.ko.yaml`, with base image `jacoblincool/workerd` (workerd runtime included)
- `build.sh --current`: builds only for the current runner platform
- Pushes images only, no release: `docker.io/fengmuxi/frp-panel:vX.Y.Z-workerd` and `:latest-workerd`

**Usage**: runs automatically after a `v*` tag is pushed. If you need the edge Worker (functions) feature, pull the variant image:

```bash
docker pull docker.io/fengmuxi/frp-panel:latest-workerd
```

> The normal image is sufficient for pure frp tunneling. To run edge Worker functions out of the box, use the `-workerd` suffixed image and avoid downloading the workerd binary at runtime.

### 5. Wiki Deploy (Docs Site Deployment)

**Trigger**: push to `main`, or manual dispatch.

**Steps**: builds the VitePress site in `docs/` with pnpm and deploys it to **GitHub Pages**.

**Usage**: docs update automatically once merged into `main`. For first-time setup, go to repository **Settings → Pages → Build and deployment → Source** and select **GitHub Actions**.

## Required Repository Secrets

Configure them under **Settings → Secrets and variables → Actions → Secrets**:

| Secret | Requirement | Purpose |
| --- | --- | --- |
| `DOCKERHUB_USERNAME` | Required | Docker Hub username used to push `docker.io/fengmuxi/frp-panel` images |
| `DOCKERHUB_TOKEN` | Required | Docker Hub access token (create at Docker Hub → Account Settings → Personal access tokens) |
| `GITHUB_TOKEN` | Auto-provided | Provided automatically by GitHub for creating releases; already referenced by the workflows |

**Impact if Docker Hub secrets are not configured**:

- **Merge Check** and **Wiki Deploy** are unaffected
- **Master Release** / **Version Tag Release** still upload release binaries, but fail at the Docker image push step
- The **workerd Docker** workflow fails entirely (it only pushes images)

## Local Equivalent Builds

The compilation steps used in the workflows can be reproduced locally via `build.sh`:

```bash
bash ./build.sh                                    # Full build (frontend + all-platform binaries)
bash ./build.sh --platform linux --arch amd64      # Build for a specific platform only
bash ./build.sh --bintype full                     # Full binary only (excludes client)
bash ./build.sh --current                          # Build for the current system (same flag as the workerd workflow)
bash ./build.sh --skip-frontend                    # Skip the frontend build
```

Output files go to the `dist/` directory.

## Issue Template

`.github/ISSUE_TEMPLATE/问题反馈-bug-report.md` is the bug report template; it loads automatically when a user clicks **New Issue** on GitHub and requires no execution.
