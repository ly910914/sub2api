# Sub2API 多台服务器更新教程（GHCR 镜像源 · 全 amd64 · 直连 GitHub）

> 适用前提（你已确认）：
> - 你改过源码，**不能**再用官方 `weishaw/sub2api:latest`，必须用你自己的镜像。
> - 在线更新源已改为 `ly910914/sub2api`（`backend/internal/service/update_service.go`）。
> - 分发方式：**推送到你的 fork 仓库镜像源（GHCR）**，每台服务器 `docker compose pull`。
> - 服务器：**全部 x86_64/amd64**，且**能直连 GitHub / GHCR**。
> - 线上用 `deploy/docker-compose.local.yml` 部署，容器名 `sub2api` / `sub2api-postgres` / `sub2api-redis`。
>
> 本机改过的项目：`D:\work\code\my\sub2api`。GitHub fork：`github.com/ly910914/sub2api`。
>
> 这份文档专门讲**多台服务器**的标准化、可批量、可灰度、可回滚的更新流程，
> 全部内容（机制、脚本、SQL 校验、回滚）已自包含在本文，无需其它文档。

---

## 目录

- [0. 一图看懂整条流水线](#0-一图看懂整条流水线)
- [1. 先搞懂「动态更新」到底是什么（两种，别混淆）](#1-先搞懂动态更新到底是什么两种别混淆)
- [2. 这次相对官方改了什么（影响升级判断）](#2-这次相对官方改了什么影响升级判断)
- [3. 一次性准备：把你的 fork 配成「能自动发镜像 + 发 Release」](#3-一次性准备把你的-fork-配成能自动发镜像--发-release)
- [4. 验证 CI 产物（发完 tag 后做一次）](#4-验证-ci-产物发完-tag-后做一次)
- [5. 首次平替：把每台服务器从官方镜像切到你的 GHCR 镜像](#5-首次平替把每台服务器从官方镜像切到你的-ghcr-镜像)
- [6. 多台服务器批量更新（标准化脚本）](#6-多台服务器批量更新标准化脚本)
- [7. 日常更新的两种节奏](#7-日常更新的两种节奏)
- [8. 回滚](#8-回滚)
- [9. 版本号与发布纪律](#9-版本号与发布纪律)
- [10. 常见问题（FAQ）](#10-常见问题faq)
- [11. 速查卡（贴墙）](#11-速查卡贴墙)
- [附录 A. 完整脚本清单](#附录-a-完整脚本清单)
- [附录 B. 完整注意事项清单（自查表）](#附录-b-完整注意事项清单自查表)
- [附录 C. 相关文件](#附录-c-相关文件)

**最短上手路径**：§3 一次性配好 fork → §4 验证产物 → §5 灰度 1 台 → §6 批量铺开。
平时更新只看 §7.1 + §11 速查卡。

---

## 0. 一图看懂整条流水线

```
┌─────────────────┐   git push + git tag v0.1.x    ┌──────────────────────────┐
│ 本机(改过的源码) │ ─────────────────────────────▶ │ github.com/ly910914/      │
│ D:\work\...\sub2api                                │ sub2api  (你的 fork)      │
└─────────────────┘                                 └────────────┬─────────────┘
                                                                 │ tag 触发
                                                                 ▼
                                          ┌───────────────────────────────────┐
                                          │ GitHub Actions (release.yml +     │
                                          │ goreleaser) 自动产出：            │
                                          │  ① GHCR 镜像（多架构 manifest）：  │
                                          │     ghcr.io/ly910914/sub2api:0.1.x│
                                          │  ② GitHub Release 归档（供网页    │
                                          │     在线更新）：                  │
                                          │     sub2api_0.1.x_linux_amd64.tar.gz│
                                          │     checksums.txt                 │
                                          └───────────────┬───────────────────┘
                                                          │ docker pull / 网页在线更新
                  ┌───────────────────────────────────────┼───────────────────────────┐
                  ▼                                        ▼                           ▼
         ┌────────────────┐                       ┌────────────────┐          ┌────────────────┐
         │  服务器 #1     │                       │  服务器 #2     │   ...    │  服务器 #N     │
         │ compose pull   │                       │ compose pull   │          │ compose pull   │
         │ up -d sub2api  │                       │ up -d sub2api  │          │ up -d sub2api  │
         └────────────────┘                       └────────────────┘          └────────────────┘
```

**一句话**：你只需在本机做一个动作——「改完代码 → 打 tag」；CI 自动出镜像和 Release；
每台服务器只需 `docker compose pull sub2api && up -d --no-deps sub2api`。和官方升级体验完全一致。

---

## 1. 先搞懂「动态更新」到底是什么（两种，别混淆）

这个项目有**两种**「动态/在线更新」，原理和适用场景完全不同：

| | A. 镜像更新（本文主力） | B. 网页在线更新（热更，临时） |
| --- | --- | --- |
| 触发 | 服务器执行 `docker compose pull` + `up` | 管理后台版本徽章点「检查更新 / 立即更新」 |
| 数据来源 | 镜像仓库（**GHCR**，本文用这个） | **GitHub Release 二进制归档**（不碰任何镜像仓库） |
| 换的是什么 | **整个容器镜像**（新二进制固化在镜像里） | 只换**容器可写层**里的 `/app/sub2api` 二进制 |
| 生效 | `up -d --no-deps sub2api` 自动起新容器即生效 | 点网页「重启」→ 进程退出 → Docker 自动拉起同容器即生效（详见 §7.2） |
| 持久性 | 永久（版本固化在镜像 tag 上） | 临时：一旦 `--force-recreate` 或改 `image` 重建，可写层丢弃 → **回退到镜像内置版本** |
| 多台批量 | ✅ 天然适合（每台 pull 同一个 tag） | ⚠️ 要逐台点按钮，不适合大规模 |
| 需不需要 Docker Hub | 不需要（GHCR 即可） | **不需要**（只用 GitHub Release 归档） |
| 适用 | **正式定版、批量统一所有服务器** | 临时给某台快速热修一下 |

### 1.1 网页在线更新的机制（代码事实）

`backend/internal/service/update_service.go`：

1. 检查更新：请求 `https://api.github.com/repos/ly910914/sub2api/releases/latest`，
   取 `tag_name` 去掉 `v` 作为「最新版本」。
2. 与当前二进制版本做 `major.minor.patch` 语义比较，**最新 > 当前**才提示「有更新」。
3. 点更新（`PerformUpdate`）：
   - 按平台拼出 `linux_amd64`，在 Release 资产里找**名字含 `linux_amd64` 且不以 `.txt` 结尾**的归档，
     再找 `checksums.txt`。
   - **只允许**从 `github.com` / `objects.githubusercontent.com`（HTTPS）下载（防 SSRF）。
   - 下载到 `/app` 同目录临时目录 → `sha256` 校验 → 从 `.tar.gz` 解出名为 `sub2api` 的二进制。
   - **原子替换**：`/app/sub2api` → `/app/sub2api.backup`，新二进制 → `/app/sub2api`（保留 backup 供回滚）。
4. 替换完成后接口返回 `need_restart: true`。此时新二进制已落到 `/app/sub2api`，但**正在运行的旧进程
   还在内存里**——需要重启进程才生效。重启有两种方式（见 §7.2）：
   - **网页自助重启**（推荐）：版本徽章里点「重启」→ 调 `/admin/system/restart` → 进程 `os.Exit(0)`
     → Docker 因 `restart: unless-stopped` 自动拉起**同一个容器**（可写层保留，跑上新二进制）→ 页面自动重连。
   - **手动重启**：`docker compose -f docker-compose.local.yml restart sub2api`。

> 镜像侧要求都已满足：根 `Dockerfile` 和 CI 用的 `Dockerfile.goreleaser` 都有
> `chown -R sub2api:sub2api /app`（允许 `sub2api` 用户原地写 `/app`）并注入
> `-X main.BuildType=release`（前端据此把 `build_type=release` 的实例放开更新入口）。所以**CI 产出的
> GHCR 镜像上，网页在线更新一样能用**；而你本机 `docker build` 出来的镜像同样注入了 release，也能用。

### 1.2 三种产物各管什么（关键概念，先理清再操作）

打一个 `v0.1.x` tag 后，CI 会产出**三类东西**，但它们用途完全不同，别混：

| 产物 | 长什么样 | 谁来消费 | 你需要它吗 |
| --- | --- | --- | --- |
| ① **GitHub Release 二进制归档** | `sub2api_0.1.x_linux_amd64.tar.gz` + `checksums.txt` | **网页「点击更新」** 下载并替换二进制 | ✅ 需要（CI 自动产出，零配置） |
| ② **GHCR 镜像** | `ghcr.io/ly910914/sub2api:0.1.x` | 服务器 `docker compose pull`（多台批量） | ✅ 需要（CI 自动产出，零配置） |
| ③ **Docker Hub 镜像** | `你的用户名/sub2api:0.1.x` | 想用 `docker pull 你的用户名/...` 时 | ❌ **不需要**（可选，跳过） |

**结论（直接回答“要不要打包到 Docker Hub”）**：

- **网页点击更新**靠的是 ①**GitHub Release 归档**，**和镜像仓库无关**——既不用 Docker Hub，也不用 GHCR。
- **多台 `docker pull`** 用 ② **GHCR** 就够了（CI 用内置 `GITHUB_TOKEN` 自动推，无需任何 secret）。
- ③ **Docker Hub 完全可选**：官方仓库额外推 Docker Hub 只是为了有个公开的 Docker Hub 页面；
  对你的「点击更新 + GHCR 多台部署」这套流程，**不配 Docker Hub 一点不影响**。
- 一句话：**你不需要自己打包到 Docker Hub。** 只要打 tag，CI 自动出 ①②，两条更新路径就都通了。

### 1.3 结论：两种更新怎么配合

- **平时**想给某一台快速验证一个小改动 → 用 B（网页点更新 + 网页点重启，全程不碰服务器命令行）。
- **正式定版 / 让 N 台统一到同一个版本** → 用 A（打 tag → 每台 `pull` + `up`，见 §6 批量脚本）。
- 不管哪种，新二进制启动都会按 `schema_migrations` 自动补跑缺失的数据库迁移，数据库不受影响。

本文第 3–6 节讲 A（多台批量），第 7 节讲 B（单台/网页热更）。

---

## 2. 这次相对官方改了什么（影响升级判断）

| 类别 | 改动 | 关联数据库迁移 |
| --- | --- | --- |
| 新增 管理员账单统计 | 新表 `admin_billing_records` + 前后端 | `145`、`146` |
| 增强 内容风控 | 风控服务/页面重写，日志加 `input_hash`/`input_text` | `147` |
| 新增/增强 图片对话 | `openai_images` 服务 + 前端 | 无 |
| 移除 管理员合规确认门禁 | 删 compliance 相关代码/前端/文档 | `151`（删 settings 行） |
| 改 在线更新源 | `update_service.go`：`githubRepo` → `ly910914/sub2api` | 无 |

- **新增的 4 个迁移**（`145`/`146`/`147`/`151`）在**新二进制首次启动时自动补跑**，无需手敲 SQL。
- `145`/`146`/`147` 是纯加表/加列（安全）；**`151` 会 `DELETE` `settings` 里的合规确认行**
  → **每台服务器更新前必须先备份数据库**（见 5.2 / 6.2）。
- `.env.example`、`config.example.yaml`、`docker-compose.local.yml` 相对官方**没改结构**，
  本次**不需要新增环境变量**，每台只需把 compose 的 `image:` 换成你的 GHCR 镜像。

---

## 3. 一次性准备：把你的 fork 配成「能自动发镜像 + 发 Release」

> 这一节**只做一次**。做完后，以后每次更新就只是「打 tag + 每台 pull」。

### 3.1 本机：把改过的代码推到你的 fork

```powershell
# Windows PowerShell，在改过的项目目录
cd D:\work\code\my\sub2api

# 确认远端指向你的 fork（没有就加）
git remote -v
# 如果 origin 还是官方仓库，改成你的 fork：
#   git remote set-url origin https://github.com/ly910914/sub2api.git
# 或新增一个名为 mine 的远端：
#   git remote add mine https://github.com/ly910914/sub2api.git

git add -A
git commit -m "custom build: 账单统计/风控/图片对话/移除合规门禁/在线更新源指向自有仓库"
git push origin main      # 若用的是 mine 远端，则 git push mine main
```

### 3.2 GitHub 仓库设置（在 `github.com/ly910914/sub2api`）

1. **启用 Actions**：fork 仓库默认可能禁用 Actions。打开仓库 → `Actions` 选项卡 →
   若提示，点 **"I understand my workflows, enable them"**。
2. **GHCR 凭据**：无需任何额外 secret。`release.yml` 用内置 `GITHUB_TOKEN` 登录
   `ghcr.io` 并推镜像（workflow 里已声明 `permissions: packages: write`）。
3. **（可选，建议跳过）DockerHub**：本文流程**不需要** DockerHub。
   - 点击更新只用 GitHub Release 归档；多台 `docker pull` 用 GHCR——两条路都不经过 DockerHub。
   - 不配 `DOCKERHUB_USERNAME` 时，CI 会自动 `skip` 推 DockerHub（`.goreleaser.yaml` 里 `skip_push` 判断），
     **不影响 GHCR 镜像和 Release 归档照常产出**。
   - 只有当你额外想要一个 `docker pull 你的用户名/sub2api` 的公开镜像时，才在
     `Settings → Secrets and variables → Actions` 加 `DOCKERHUB_USERNAME` + `DOCKERHUB_TOKEN`，
     并把线上 compose 的 `image:` 换成 `你的用户名/sub2api:0.1.x`。否则保持用 GHCR 即可。

### 3.3 第一次发版：打 tag 触发 CI

```powershell
cd D:\work\code\my\sub2api

# VERSION 当前已是 0.1.137（backend/cmd/server/VERSION）。
# 规则：tag 版本要 > 线上正在跑的官方版本（官方 0.1.136），这样网页才认为"有更新"。
git tag v0.1.137
git push origin v0.1.137      # 若用 mine 远端：git push mine v0.1.137
```

`release.yml` 触发条件是 `push tags: v*`。它会：构建前端 → goreleaser 编译多架构二进制 →
产出 `sub2api_0.1.137_linux_amd64.tar.gz`（+ arm64）+ `checksums.txt` → 创建 **正式
Release**（`draft: false`）→ 推 GHCR 镜像 `ghcr.io/ly910914/sub2api:0.1.137`（amd64+arm64 manifest）→
回写 VERSION 到默认分支。

> ⚠️ **关于 pre-release**：`.goreleaser.yaml` 配的是 `prerelease: auto`——只要 tag **不带**
> `-rc`/`-beta` 之类后缀（即纯 `v0.1.137`），就是**正式版、非 pre-release**。
> 网页在线更新查的是 `/releases/latest`，**只返回正式、非预发布**的最新版，所以务必用纯版本号 tag。

### 3.4 ⭐ 把 GHCR 镜像包设为 Public（关键，否则服务器 pull 会 401）

GHCR 新推的包**默认是 Private**。服务器直接 `docker pull` 私有包会失败。二选一：

- **方案一（推荐，最省事）**：设为 Public。
  打开 `https://github.com/users/ly910914/packages/container/sub2api/settings`
  → `Danger Zone` → **Change visibility** → **Public**。
  之后任何机器都能匿名 `docker pull ghcr.io/ly910914/sub2api:0.1.137`。
- **方案二（想保持私有）**：每台服务器先登录 GHCR。
  在 GitHub 生成一个 **classic PAT**（勾 `read:packages`），然后每台：
  ```bash
  echo '<你的PAT>' | docker login ghcr.io -u ly910914 --password-stdin
  ```

> 镜像里**不含**任何数据库密码/密钥（密码都在服务器 `.env` 里），设 Public 不会泄露敏感信息。
> 但你的**源码**本身若仓库是 private，设镜像 Public 只暴露二进制，不暴露源码。按需取舍。

---

## 4. 验证 CI 产物（发完 tag 后做一次）

在 GitHub 仓库 `Actions` 看 `Release` 这条 workflow 跑成功（绿勾），然后核对：

1. **Release 页**：`github.com/ly910914/sub2api/releases/tag/v0.1.137` 应能看到资产：
   - `sub2api_0.1.137_linux_amd64.tar.gz`
   - `sub2api_0.1.137_linux_arm64.tar.gz`
   - `checksums.txt`
   - 且**不是 Draft、不是 Pre-release**。

2. **GHCR 镜像**（在任意能直连的机器上，本机或一台服务器）：
   ```bash
   docker manifest inspect ghcr.io/ly910914/sub2api:0.1.137 | grep -A2 architecture
   # 应看到 amd64（和 arm64）。能 inspect 成功 = 包可访问（Public 或已登录）。
   ```

3. **在线更新 API**（确认 latest 指向 0.1.137）：
   ```bash
   curl -s https://api.github.com/repos/ly910914/sub2api/releases/latest | grep '"tag_name"'
   # 期望： "tag_name": "v0.1.137"
   ```

只有这 3 项都 OK，才进入第 5 节去动线上服务器。

---

## 5. 首次平替：把每台服务器从官方镜像切到你的 GHCR 镜像

> 这是**一次性切换**（官方 `weishaw/sub2api:latest` → `ghcr.io/ly910914/sub2api:0.1.137`）。
> 先讲**单台手动**流程（便于理解每步在干什么），第 6 节给**多台批量脚本**。
> 强烈建议：**先在 1 台（灰度机）跑通整个第 5 节，确认无误，再用第 6 节脚本铺开到其余服务器。**

下面假设单台服务器部署目录 `/opt/sub2api-deploy`（按你实际改）。

### 5.1 进入目录、加载 .env、记录回滚信息

```bash
ssh user@server1
cd /opt/sub2api-deploy
set -a; . ./.env; set +a                          # 加载 .env 到当前 shell

docker compose -f docker-compose.local.yml ps
docker exec sub2api /app/sub2api --version        # 记录旧版本（应是 0.1.136）
docker inspect sub2api --format '{{.Image}}'      # 记录旧镜像 ID（回滚备用）
```

### 5.2 ⚠️ 备份（必做，因为 `151` 会删 settings 行）

```bash
backup_ts="$(date +%Y%m%d-%H%M%S)"
mkdir -p backups

# 逻辑备份数据库
docker exec sub2api-postgres pg_dump \
  -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" \
  -Fc -f "/tmp/sub2api-${backup_ts}.dump"
docker cp "sub2api-postgres:/tmp/sub2api-${backup_ts}.dump" "./backups/sub2api-${backup_ts}.dump"

# 备份配置与应用数据目录
tar czf "backups/sub2api-cfg-${backup_ts}.tar.gz" .env docker-compose.local.yml data
```

### 5.3 改 compose 的 image 指向你的 GHCR 镜像

```bash
cp docker-compose.local.yml "docker-compose.local.yml.bak-${backup_ts}"

# 把 sub2api 服务的 image 从官方换成你的 GHCR 固定版本 tag
sed -i 's#^\(\s*image:\s*\)weishaw/sub2api:latest#\1ghcr.io/ly910914/sub2api:0.1.137#' docker-compose.local.yml

# 校验改对了 + compose 可解析
grep -nE 'image:\s*ghcr.io/ly910914/sub2api' docker-compose.local.yml
docker compose -f docker-compose.local.yml config >/dev/null && echo "compose OK"
```

> 建议**固定版本号 tag**（`:0.1.137`），不要用 `:latest`。固定 tag 才能精确控制每台版本、便于回滚。

### 5.4 只拉取并重建应用容器（绝不动 postgres/redis）

```bash
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
docker compose -f docker-compose.local.yml logs -f --tail=200 sub2api
```

> 用 `pull sub2api` + `up -d --no-deps sub2api`。**不要**用无范围的 `docker compose pull` /
> 全量 `up`，否则会顺带升级 `postgres:18-alpine` / `redis:8-alpine`。
> 日志里应能看到 4 个迁移逐个 `applied`，最后监听 8080。

### 5.5 验证

```bash
docker compose -f docker-compose.local.yml ps
curl -fsS "http://127.0.0.1:${SERVER_PORT:-8080}/health"
docker exec sub2api /app/sub2api --version        # 应显示 0.1.137

# 核对 4 个迁移已执行
docker exec -i sub2api-postgres psql -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -c \
"SELECT filename FROM schema_migrations
 WHERE filename IN ('145_admin_billing_records.sql','146_admin_billing_record_source.sql',
 '147_content_moderation_log_input_content.sql','151_remove_admin_compliance_acknowledgements.sql')
 ORDER BY filename;"     # 应返回 4 行
```

再登录网页确认：管理员账单页、风控页、图片对话页正常；合规确认弹窗已消失；
用真实 API Key 走一笔小流量验证网关。

**灰度机（server1）这套全过了，才进入第 6 节铺开。**

---

## 6. 多台服务器批量更新（标准化脚本）

> 控制端可以是你本机的 **Git Bash / WSL**，或任意一台能 SSH 到所有服务器的 Linux「跳板机」。

### 6.0 前置条件 + 脚本清单（先看这里）

**前置条件：**

1. **SSH 免密**：控制端能用 SSH key 免密登录每台服务器（`ssh-copy-id user@host` 配好）。
2. **目录结构一致**：每台 `docker-compose.local.yml` + `.env` + `data/` 结构一致；目录路径不同就写进 `servers.txt` 第二列。
3. **⭐ 换行符必须是 LF，不能是 CRLF**：你在 Windows 上创建的 `.sh` 脚本和 `servers.txt` 默认是
   **CRLF（`\r\n`）**，传到 Linux 后 `while read` 会把 `\r` 读进目录名、heredoc 也会错乱，**直接报莫名其妙的错**。
   解决其一：
   - 用 VS Code 右下角把行尾改成 **LF** 再保存；或
   - 在 Linux 上 `sed -i 's/\r$//' fleet-*.sh servers.txt`；或
   - 装了 dos2unix 则 `dos2unix fleet-*.sh servers.txt`。
4. **私有 GHCR 要先登录**：若你**没**按 3.4 把 GHCR 包设为 Public，则 `fleet-update.sh` 里的 `pull` 会 `401`。
   先给每台登录一次（一次性）：
   ```bash
   # 控制端，对清单每台执行（PAT 勾 read:packages）
   while read -r t d _; do [[ -z "$t" || "$t" == \#* ]] && continue;
     ssh "$t" "echo '<你的PAT>' | docker login ghcr.io -u ly910914 --password-stdin"; done < servers.txt
   ```
   设了 Public 就**跳过这步**。
5. **滚动停机意识**：`up -d --no-deps sub2api` 重建应用容器有**秒级停机**。多台在负载均衡/反代后面时，
   脚本是**逐台串行**执行（不是并发），天然就是滚动更新；但建议仍按 6.5 先灰度 1 台。

**本文用到的脚本（控制端，和 `servers.txt` 放同一目录）：**

| 脚本 | 作用 | 出处 |
| --- | --- | --- |
| `servers.txt` | 服务器清单（你手建） | 6.1 |
| `fleet-backup.sh` | 批量备份数据库+配置 | 6.2 |
| `fleet-update.sh` | 批量切镜像+pull+重建+等health | 6.3 |
| `fleet-verify.sh` | 批量巡检 health/版本/迁移 | 6.4 |

> 仓库里 `deploy/build_image.sh`（已提交）可在你想**本机 build** 而非用 CI/GHCR 时备用。
> 数据库迁移由应用启动自动跑，**无需手工执行 SQL**；如需在单台精细核对，用下面这段**内联校验 SQL**即可：

```bash
# 单台更新后精细核对（只读，无副作用）。先 ssh 进该服务器：
ssh user@serverX
cd /opt/sub2api-deploy && set -a; . ./.env; set +a
docker exec -i sub2api-postgres psql -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" <<'SQL'
-- 1) 4 个新增迁移是否都已 APPLIED（期望 4 行）
SELECT filename FROM schema_migrations
 WHERE filename IN ('145_admin_billing_records.sql','146_admin_billing_record_source.sql',
 '147_content_moderation_log_input_content.sql','151_remove_admin_compliance_acknowledgements.sql')
 ORDER BY filename;
-- 2) 新表是否存在（期望 1）
SELECT count(*) AS admin_billing_records_exists FROM information_schema.tables
 WHERE table_name='admin_billing_records';
-- 3) 风控日志新列是否存在（期望 2）
SELECT count(*) AS moderation_new_cols FROM information_schema.columns
 WHERE table_name='content_moderation_logs' AND column_name IN ('input_hash','input_text');
-- 4) 合规确认设置是否已清空（期望 0）
SELECT count(*) AS compliance_settings_left FROM settings
 WHERE key='admin_compliance_acknowledgement' OR key LIKE 'admin_compliance_acknowledgement:%';
SQL
```

### 6.1 建服务器清单 `servers.txt`

每行一台：`SSH目标 部署目录`（空格分隔）。`#` 开头为注释。

```text
# user@host                  deploy_dir
user@server1.example.com     /opt/sub2api-deploy
user@server2.example.com     /opt/sub2api-deploy
user@10.0.0.13               /home/ubuntu/sub2api-deploy
```

下面三个脚本放在控制端同一目录，和 `servers.txt` 一起。把 `IMAGE` 改成你这次要发布的版本。

### 6.2 批量备份脚本 `fleet-backup.sh`

```bash
#!/usr/bin/env bash
# 对清单里每台服务器做数据库 + 配置备份。更新前先全量跑一遍。
set -euo pipefail
SERVERS_FILE="${1:-servers.txt}"

while read -r target dir _; do
  [[ -z "$target" || "$target" == \#* ]] && continue
  echo "==== [$target] 备份开始 ($dir) ===="
  ssh "$target" "bash -se" <<REMOTE
    set -euo pipefail
    cd "$dir"
    set -a; . ./.env; set +a
    ts="\$(date +%Y%m%d-%H%M%S)"
    mkdir -p backups
    docker exec sub2api-postgres pg_dump -U "\${POSTGRES_USER:-sub2api}" -d "\${POSTGRES_DB:-sub2api}" -Fc -f "/tmp/sub2api-\${ts}.dump"
    docker cp "sub2api-postgres:/tmp/sub2api-\${ts}.dump" "./backups/sub2api-\${ts}.dump"
    tar czf "backups/sub2api-cfg-\${ts}.tar.gz" .env docker-compose.local.yml data
    cp docker-compose.local.yml "docker-compose.local.yml.bak-\${ts}"
    echo "[OK] \$target 备份完成: backups/sub2api-\${ts}.dump"
REMOTE
done < "$SERVERS_FILE"
echo "==== 全部备份完成 ===="
```

### 6.3 批量更新脚本 `fleet-update.sh`

```bash
#!/usr/bin/env bash
# 把每台服务器的 sub2api 镜像切到指定 GHCR 版本并只重建应用容器。
set -euo pipefail
SERVERS_FILE="${1:-servers.txt}"
IMAGE="${2:-ghcr.io/ly910914/sub2api:0.1.137}"   # 第二个参数可覆盖目标镜像

while read -r target dir _; do
  [[ -z "$target" || "$target" == \#* ]] && continue
  echo "==== [$target] 更新到 $IMAGE ($dir) ===="
  ssh "$target" "IMAGE='$IMAGE' DIR='$dir' bash -se" <<'REMOTE'
    set -euo pipefail
    cd "$DIR"
    set -a; . ./.env; set +a

    # 1) 改 image 为目标版本（兼容旧官方/旧自定义两种写法）
    sed -i -E "s#^(\s*image:\s*).*sub2api.*#\1${IMAGE}#" docker-compose.local.yml
    grep -nE "image:\s*${IMAGE//\//\\/}" docker-compose.local.yml
    docker compose -f docker-compose.local.yml config >/dev/null

    # 2) 只拉应用镜像 + 只重建应用容器（不动 postgres/redis）
    docker compose -f docker-compose.local.yml pull sub2api
    docker compose -f docker-compose.local.yml up -d --no-deps sub2api

    # 3) 等健康
    for i in $(seq 1 30); do
      if curl -fsS "http://127.0.0.1:${SERVER_PORT:-8080}/health" >/dev/null 2>&1; then
        echo "[OK] health 通过"; break
      fi
      sleep 2
      [ "$i" = "30" ] && { echo "[FAIL] health 60s 未通过"; docker compose -f docker-compose.local.yml logs --tail=80 sub2api; exit 1; }
    done

    echo "[VERSION] $(docker exec sub2api /app/sub2api --version)"
REMOTE
  echo "==== [$target] 完成 ===="
done < "$SERVERS_FILE"
echo "==== 全部更新完成 ===="
```

> ⚠️ `fleet-update.sh` 里的 `sed` 假设 compose 中**只有一行** `image:` 含 `sub2api`（即应用服务那行）。
> `postgres:18-alpine` / `redis:8-alpine` 不含 `sub2api`，不会被误改；注释掉的 `# image:` 也不会被匹配。
> 但如果你手改过 compose、出现第二行含 sub2api 的 image，请先确认。脚本里 `grep` + `config` 两步就是用来当场暴露改错的。

### 6.4 批量验证脚本 `fleet-verify.sh`

```bash
#!/usr/bin/env bash
# 巡检每台：健康 / 版本 / 4 个迁移是否齐。
set -euo pipefail
SERVERS_FILE="${1:-servers.txt}"

while read -r target dir _; do
  [[ -z "$target" || "$target" == \#* ]] && continue
  echo "==== [$target] 巡检 ===="
  ssh "$target" "DIR='$dir' bash -se" <<'REMOTE'
    set -euo pipefail
    cd "$DIR"; set -a; . ./.env; set +a
    echo -n "health: "; curl -fsS "http://127.0.0.1:${SERVER_PORT:-8080}/health" && echo
    echo -n "version: "; docker exec sub2api /app/sub2api --version
    echo "migrations:"
    docker exec -i sub2api-postgres psql -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -tA -c \
"SELECT count(*) FROM schema_migrations
 WHERE filename IN ('145_admin_billing_records.sql','146_admin_billing_record_source.sql',
 '147_content_moderation_log_input_content.sql','151_remove_admin_compliance_acknowledgements.sql');"
    echo "(↑ 期望 4)"
REMOTE
done < "$SERVERS_FILE"
```

### 6.5 推荐执行顺序（灰度 → 全量）

```bash
chmod +x fleet-*.sh

# 1) 全量备份
./fleet-backup.sh servers.txt

# 2) 灰度：先只更 1 台（临时清单里只放 server1）
printf 'user@server1.example.com /opt/sub2api-deploy\n' > servers.canary.txt
./fleet-update.sh servers.canary.txt ghcr.io/ly910914/sub2api:0.1.137
./fleet-verify.sh servers.canary.txt
#   → 人工登录这台网页确认业务无异常

# 3) 全量铺开（其余服务器）
./fleet-update.sh servers.txt ghcr.io/ly910914/sub2api:0.1.137
./fleet-verify.sh servers.txt
```

---

## 7. 日常更新的两种节奏

### 7.1 节奏 A（推荐 · 正式定版 · 多台统一）

以后每次改完代码：

```powershell
# 本机
cd D:\work\code\my\sub2api
git add -A && git commit -m "xxx 修改"
echo 0.1.138 > backend\cmd\server\VERSION    # 升一位
git add backend/cmd/server/VERSION && git commit -m "chore: VERSION 0.1.138"
git push origin main
git tag v0.1.138 && git push origin v0.1.138  # 触发 CI
```

CI 跑完后，控制端一行铺开所有服务器：

```bash
./fleet-backup.sh servers.txt
./fleet-update.sh servers.txt ghcr.io/ly910914/sub2api:0.1.138
./fleet-verify.sh servers.txt
```

### 7.2 节奏 B（临时 · 单台 · 网页点击更新）

> 这就是你问的「点击更新」。它**只依赖 GitHub Release 归档，不碰 Docker Hub / GHCR**。
> 全程可在网页完成，不重建容器、不改 compose、不用敲服务器命令。

**前提（一次性，已满足就忽略）：**

- 该实例是 **release 版**（镜像由根 `Dockerfile` 或 CI 构建，已注入 `BuildType=release`）。
  只有 release 版网页才显示更新按钮；本机 `docker build` / GHCR 镜像都满足。
- 你的 fork 上有**比当前版本更高**的正式 Release（非 draft、非 pre-release）。打 tag 即自动产出。
- 容器有重启策略 `restart: unless-stopped`（`docker-compose.local.yml` 默认就有）——否则网页重启后不会自动拉起。
- 该台能访问 `api.github.com` 和 `objects.githubusercontent.com`（你的服务器可直连，满足）。

**操作步骤（全在网页里）：**

1. 用管理员登录该台 Web 后台，看页面里的**版本徽章**（`VersionBadge`，通常在右上角/顶栏或系统页）。
2. 点 **检查更新**。后端请求 `https://api.github.com/repos/ly910914/sub2api/releases/latest`，
   若最新版 > 当前版，徽章会提示「有新版本」。
3. 点 **立即更新**。后端 `POST /admin/system/update`：
   - 从 Release 找 `sub2api_<新版>_linux_amd64.tar.gz` + `checksums.txt`；
   - 仅从 `github.com` / `objects.githubusercontent.com`（HTTPS）下载 → `sha256` 校验 → 解出 `sub2api`；
   - **原子替换** `/app/sub2api`（旧的存成 `/app/sub2api.backup`）。
   - 完成后返回 `need_restart: true`。
4. 点 **重启**（按钮会在更新成功后出现）。后端 `POST /admin/system/restart` → 进程 `os.Exit(0)`
   → Docker 因 `restart: unless-stopped` **自动拉起同一个容器**（可写层保留 → 跑上新二进制）
   → 前端倒计时几秒后自动重连刷新。
5. 重连后再点一次 **检查更新**，确认当前版本 = 最新版，即热更成功。

> 也可用命令行重启代替第 4 步：
> `ssh user@serverX 'cd /opt/sub2api-deploy && docker compose -f docker-compose.local.yml restart sub2api'`

**重要特性与边界：**

- **热更是临时的**：它改的是容器**可写层**。之后任何 `docker compose up -d --force-recreate`
  或改 `image` 重建容器，可写层被丢弃 → **回退到镜像内置版本**。所以正式定版仍要走节奏 A，让镜像 tag 追上。
- **不需要 Docker Hub / GHCR**：点击更新只读 GitHub Release 归档。镜像仓库只服务于节奏 A 的 `docker pull`。
- **多台不建议逐台点**：N 台都想升时，直接走节奏 A（一次打 tag、批量 `pull`）比逐台点网页省事得多。
- **回滚**：见 §8.3（最简单是镜像回滚，重建容器即回到镜像内置版本）。

---

## 8. 回滚

### 8.1 镜像回滚（首选，最快）—— 单台或批量

把目标镜像换回上一版本（比如从 `0.1.138` 回到 `0.1.137`），重跑更新脚本即可：

```bash
# 批量回滚整队到上一版本
./fleet-update.sh servers.txt ghcr.io/ly910914/sub2api:0.1.137
./fleet-verify.sh servers.txt
```

单台手动回滚：

```bash
ssh user@serverX
cd /opt/sub2api-deploy
sed -i -E 's#^(\s*image:\s*).*sub2api.*#\1ghcr.io/ly910914/sub2api:0.1.137#' docker-compose.local.yml
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
```

- `145`/`146`/`147` 是纯加表/加列，旧二进制会忽略 → **镜像回滚通常不需要恢复数据库**。
- `151` 删了 `settings` 的合规确认行，回退旧二进制后最多再弹一次合规确认弹窗（不是丢业务数据）。

### 8.2 数据库回滚（仅当迁移本身出问题时，用 5.2/6.2 的备份）

```bash
ssh user@serverX
cd /opt/sub2api-deploy; set -a; . ./.env; set +a
docker compose -f docker-compose.local.yml stop sub2api
docker cp "./backups/sub2api-<ts>.dump" "sub2api-postgres:/tmp/restore.dump"
docker exec -i sub2api-postgres pg_restore -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" \
  --clean --if-exists "/tmp/restore.dump"
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
```

### 8.3 网页热更的回滚

热更替换时保留了 `/app/sub2api.backup`。代码里有 `Rollback`（重命名 backup 回 `sub2api`），
但 Docker 下最简单可靠的回滚就是 **8.1 镜像回滚**——重建容器丢弃可写层即回到镜像内置版本。

---

## 9. 版本号与发布纪律

- 部署版本 = 镜像 tag = `backend/cmd/server/VERSION`。CI 打 `v0.1.x` 时会把 VERSION 同步成 `0.1.x`。
- 网页在线更新比较「当前二进制版本」vs「fork 上 `/releases/latest` 的版本」，**新 > 旧**才提示。
- **纪律**：
  1. 每次发布**版本号必须递增**（`0.1.137 → 0.1.138`），否则网页不认为有更新、镜像 tag 也会撞。
  2. tag 用**纯版本号**（`v0.1.138`），别带 `-rc`/`-beta`（否则被当 pre-release，`/releases/latest` 取不到）。
  3. Release 必须是**已发布、非 draft、非 pre-release**（goreleaser 默认即满足）。
  4. **绝不修改已经跑过的旧迁移文件**——`schema_migrations` 存了 checksum，改了会导致应用**拒绝启动**。
     要改库结构，**永远新增**一个更大编号的迁移文件。

---

## 10. 常见问题（FAQ）

**Q1. 服务器 `docker pull` 报 `denied` / `401 unauthorized`？**
GHCR 包还是 Private。按 3.4 设为 Public，或在该服务器 `docker login ghcr.io`。

**Q2. 升级后日志 `checksum mismatch`，应用拒绝启动？**
线上 `schema_migrations` 里某个旧迁移的 checksum 和新镜像里同名文件不一致——说明有人改过已应用的旧迁移。
**不要**强改库。核对是哪一个文件被改动，恢复其原始内容重新构建/发布，再升级。本次新增的 4 个都是新文件，正常不会触发。

**Q3. 我能不能不打 tag，直接在服务器上 `git pull` 重新 build 镜像？**
可以，但**不推荐多台场景**：那样每台都要装 Go/Node 工具链、各编各的、还可能编出不一致的二进制。
GHCR 方案是「编一次、N 台拉同一个 manifest」，可复现、可回滚、最省事。

**Q4. 我需不需要自己打包到 Docker Hub？（尤其是为了网页「点击更新」）**
**不需要。** 这是最常见的误解，分清楚：
- **网页「点击更新」** 下载的是 **GitHub Release 里的二进制归档**（`sub2api_<版本>_linux_amd64.tar.gz`
  + `checksums.txt`），**根本不经过任何镜像仓库**——既不用 Docker Hub，也不用 GHCR。
  你只要打 tag、CI 出了正式 Release，点击更新就能用。
- **多台 `docker pull` 部署** 用 **GHCR**（`ghcr.io/ly910914/sub2api`）就够了，CI 用内置
  `GITHUB_TOKEN` 自动推，**不需要配任何 secret**。
- **Docker Hub 是纯可选**：官方仓库推 Docker Hub 只是为了有个公开页面。对你这套
  「点击更新 + GHCR 多台」流程，不配 Docker Hub **完全不影响**。详见 §1.2 的三产物对照表。

**Q4b. 那官方仓库为什么要推 Docker Hub？我 fork 后要照做吗？**
官方为了让任何人都能 `docker pull weishaw/sub2api` 才推 Docker Hub。你 fork 自用没这个需求——
GHCR 同样能 `docker pull`，且 CI 默认就推。除非你特别想要 Docker Hub 上的镜像，否则跳过（见 §3.2）。

**Q5. 升级会不会把 PostgreSQL / Redis 也升了？**
只要你坚持用 `pull sub2api` + `up -d --no-deps sub2api`（脚本里就是这么写的），**不会**。
绝不要用无范围 `docker compose pull` 当「只更 sub2api」的命令。

**Q6. 多台之间数据是独立的吗？批量更新会互相影响吗？**
每台有自己独立的 `postgres_data` / `redis_data` / `data`。批量脚本是逐台 SSH 各自 `pull`+`up`，
互不影响。每台各自启动时按自己的 `schema_migrations` 补跑迁移。

**Q7. 直连 GitHub 偶尔抖动导致网页在线更新失败？**
本文前提是「能直连」，但若偶发失败，可在该台 `.env` 配 `UPDATE_PROXY_URL=socks5h://127.0.0.1:1080`
后 `up -d --no-deps sub2api` 生效。镜像 pull 抖动则重试 `docker compose pull sub2api` 即可。

**Q8. 不同服务器端口不同怎么办？**
脚本里 health 检查用的是各台 `.env` 里的 `SERVER_PORT`（默认 8080），已自动适配。
部署目录不同就写进 `servers.txt` 第二列。

**Q9. 脚本在 Linux 上报 `$'\r': command not found` / `No such file or directory`？**
你的 `.sh` 或 `servers.txt` 是 Windows 的 CRLF 换行。按 6.0 第 3 条转成 LF（`sed -i 's/\r$//' fleet-*.sh servers.txt`）。

**Q10. 每台 `backups/` 会不会越堆越多撑爆磁盘？**
会。每次更新都生成一份 dump + tar。定期清理旧备份，例如各台保留最近 7 份：
```bash
ssh user@serverX 'cd /opt/sub2api-deploy/backups && ls -1t sub2api-*.dump | tail -n +8 | xargs -r rm -f'
```

**Q11. 多台在负载均衡后面，更新时会不会整体抖一下？**
`fleet-update.sh` 是**逐台串行**，每台只在自己 `up -d --no-deps sub2api` 那几秒不可用（健康检查通过后才轮下一台），
相当于滚动更新。要更稳就先灰度 1 台（6.5），确认无误再铺其余。`postgres`/`redis` 全程不动，无停机。

**Q12. 需要手工执行 SQL 迁移吗？**
**不需要**。4 个迁移由应用启动时自动跑（按 `schema_migrations` 补缺）。
只想**只读核对**结果时，用 §6.0 那段内联校验 SQL 即可，无副作用。

---

## 11. 速查卡（贴墙）

```bash
# —— 本机发版（一次）——
git add -A && git commit -m "..." && git push origin main
echo 0.1.x > backend/cmd/server/VERSION && git commit -am "VERSION 0.1.x" && git push
git tag v0.1.x && git push origin v0.1.x        # CI 自动出 GHCR 镜像 + Release

# —— 控制端铺开所有服务器 ——
./fleet-backup.sh servers.txt
./fleet-update.sh servers.txt ghcr.io/ly910914/sub2api:0.1.x
./fleet-verify.sh servers.txt

# —— 回滚整队到上一版 ——
./fleet-update.sh servers.txt ghcr.io/ly910914/sub2api:0.1.(x-1)

# —— 单台只更应用（手动）——
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
```

---

## 附录 A. 完整脚本清单

### A.1 控制端批量脚本（本文档第 6 节，复制即用）

| 脚本 | 作用 | 章节 |
| --- | --- | --- |
| `servers.txt` | 服务器清单（手建，每行 `user@host  部署目录`） | 6.1 |
| `fleet-backup.sh` | 批量备份数据库 + 配置 + 备份 compose | 6.2 |
| `fleet-update.sh` | 批量切镜像 + `pull sub2api` + `up --no-deps` + 等 health + 报版本 | 6.3 |
| `fleet-verify.sh` | 批量巡检 health / 版本 / 4 个迁移是否齐 | 6.4 |
| 私有 GHCR 一次性登录 | 仅当未设 Public 时，给每台 `docker login ghcr.io` | 6.0 |
| 灰度→全量执行顺序 | 先 1 台 canary，再全量 | 6.5 |

### A.2 仓库内可选脚本 + 内联校验

| 项 | 作用 | 说明 |
| --- | --- | --- |
| `deploy/build_image.sh`（已提交） | 本机构建镜像 | 仅在想本机 build 而非用 CI/GHCR 时备用 |
| 内联校验 SQL（见 §6.0） | 更新后只读核对 4 迁移/新表/新列/合规清空 | 只读、无副作用，无需外部 SQL 文件 |

### A.3 单台手动命令（散落各节）

| 操作 | 命令要点 | 章节 |
| --- | --- | --- |
| 发版 | `git tag v0.1.x && git push origin v0.1.x` | 3.3 / 7.1 |
| 备份 | `pg_dump -Fc` + `docker cp` + `tar` | 5.2 |
| 切镜像 | `sed` 改 `image:` → `ghcr.io/ly910914/sub2api:0.1.x` | 5.3 |
| 只更应用 | `pull sub2api` + `up -d --no-deps sub2api` | 5.4 |
| 验证 | `/health` + `--version` + 查 `schema_migrations` | 5.5 |
| 网页热更 | 网页点「检查更新」→「立即更新」→「重启」（全程网页自助） | 7.2 |
| 回滚镜像 | `sed` 改回上一版 tag → `pull` + `up` | 8.1 |
| 回滚数据库 | `pg_restore --clean --if-exists` | 8.2 |

---

## 附录 B. 完整注意事项清单（自查表）

| # | 注意事项 | 章节 |
| --- | --- | --- |
| 0 | **不需要 Docker Hub**：点击更新只用 GitHub Release 归档；多台 pull 用 GHCR | §1.2 / §3.2 / Q4 |
| 1 | **两种动态更新别混**：镜像更新（永久）vs 网页热更（临时） | §1 |
| 2 | 网页热更是临时的，`--force-recreate`/改 image 重建即**回退到镜像内置版本** | §1.1 / §7.2 |
| 3 | 网页热更后需**重启进程**才生效：网页点「重启」(`os.Exit`→Docker 自动拉起) 或 `docker restart sub2api` | §1.1 / §7.2 |
| 4 | `151` 会 `DELETE` `settings` 行 → **每台更新前必须备份** | §2 / §5.2 / §6.2 |
| 5 | ⭐ **GHCR 包默认 Private，要设 Public**，否则服务器 `pull` 报 401 | §3.4 / Q1 |
| 6 | tag 用**纯版本号**（`v0.1.x`），别带 `-rc`/`-beta`（否则被当 pre-release，`/releases/latest` 取不到） | §3.3 / §9 |
| 7 | **只 `pull sub2api`**，绝不用无范围 `docker compose pull`（会升级 pg/redis） | §5.4 / §6.3 / Q5 |
| 8 | 用**固定版本 tag**（`:0.1.x`），别用 `:latest`，便于精确控制与回滚 | §5.3 |
| 9 | **版本号每次必须递增**，否则网页不提示更新、镜像 tag 撞车 | §9 / Q2 |
| 10 | **绝不修改已跑过的旧迁移文件**（checksum 不符会拒绝启动）；改库永远新增更大编号文件 | §9 / Q2 |
| 11 | ⭐ **CRLF→LF 换行符**：Windows 建的 `.sh`/`servers.txt` 要转成 LF | §6.0 / Q9 |
| 12 | 私有 GHCR 时，fleet 脚本前需先给每台 `docker login ghcr.io` | §6.0 |
| 13 | 每台 `up -d --no-deps sub2api` 有**秒级停机**，逐台串行=滚动更新，先灰度 1 台 | §6.0 / §6.5 / Q11 |
| 14 | `fleet-update.sh` 的 `sed` 假设 compose 中**只有一行** `image:` 含 `sub2api` | §6.3 |
| 15 | 各台 `backups/` 会堆积，**定期清理**旧备份 | Q10 |
| 16 | 直连 GitHub 偶发抖动可配 `UPDATE_PROXY_URL`（网页热更）/ 重试 `pull`（镜像） | §5.7-类比 / Q7 |
| 17 | 不同端口/目录自动适配：`SERVER_PORT` + `servers.txt` 第二列 | Q8 |
| 18 | 多台数据独立（各自 `postgres_data`/`redis_data`/`data`），批量互不影响 | Q6 |

---

## 附录 C. 相关文件

本文已自包含「多台服务器更新」的全部内容（机制 / 脚本 / SQL 校验 / 回滚）。
仓库内仍可参考的**已提交**文件：

| 文件 | 用途 |
| --- | --- |
| `deploy/README.md` | 项目通用部署说明（Docker / 二进制安装） |
| `deploy/DOCKER.md` | 镜像与环境变量说明 |
| `deploy/docker-compose.local.yml` | 线上使用的 compose（本地目录版） |
| `deploy/build_image.sh` | 本机构建镜像（CI/GHCR 之外的备用） |
