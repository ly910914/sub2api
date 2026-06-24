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
- `.env.example`、`config.example.yaml`、`docker-compose.local.yml` 相对官方**没改结构**，
  本次**不需要新增环境变量**，每台只需把 compose 的 `image:` 换成你的 GHCR 镜像。

### 2.1 更新前要备份什么（大库**无需全量** pg_dump）

**应用更新（换镜像/换二进制）根本不碰 `postgres_data`**，真正动数据库的只有「新二进制启动时跑的迁移」。
逐个看 4 个迁移对数据的风险：

| 迁移 | 动作 | 对已有数据的风险 |
| --- | --- | --- |
| `145` | 新建表 `admin_billing_records` | 纯新增，**零风险** |
| `146` | 给新表加列 `source` | 纯新增，**零风险** |
| `147` | 给 `content_moderation_logs` 加 `input_hash`/`input_text` 两列 + 回填 | 加列 + 填新列，**不删旧数据** |
| `151` | `DELETE` `settings` 里几行合规配置 | **唯一的删除**，且只删 `settings` 几行配置 |

**结论：唯一会删东西的是 `151`，删的是 `settings` 表里几行配置。** 所以**只需备份 `settings` 这一张小表**
即可覆盖全部回滚风险——秒级完成、几 KB 大小。**不需要为一次应用更新去全量 dump 巨型库。**

> 镜像回滚也不需要恢复数据库：`145/146/147` 是加表/加列，旧二进制直接忽略；
> `151` 删的配置回退后管理员再确认一次即可（不是丢业务数据）。

三种备份模式（本文脚本用环境变量 `BACKUP_MODE` 控制，默认 `light`）：

| 模式 | 备份内容 | 适用 |
| --- | --- | --- |
| `light`（默认） | 只 `pg_dump -t settings` + 导出 `schema_migrations` 清单 | 本次及任何「仅新增/仅删 settings」型更新——大库首选 |
| `full` | 完整 `pg_dump -Fc` | 库不大、或迁移会动核心业务表时 |
| `none` | 不备份 | 你已有独立全量备份/快照时 |

**巨型库的“完整备份”不要绑在每次更新上**，独立、低峰期做，且别用 `pg_dump`：

- **卷快照**（最适合大库）：`postgres_data` 若在 LVM/ZFS/云盘上，用快照——近乎瞬时、增量、省空间。
- **pgBackRest / WAL-G**：增量备份 + 时间点恢复（PITR），大库的正解，搭好后再不用全量 dump。
- 临时全量：`pg_dump -Fd -j4 -Z6`（目录格式 + 并行 + 压缩）比单文件快得多。

### 2.2 ⚠️ 大库的停机提醒：`147` 的回填

`147` 会对 `content_moderation_logs` 做一次回填 `UPDATE`（从 `input_excerpt` 填 `input_text`）。
**如果你这张风控日志表非常大**，这次首启会在跑迁移时卡一会儿，**期间应用不可用**（应用要等迁移跑完才对外服务）。
建议：**低峰期**更新；**先在一台灰度**观察启动耗时，再推其余。`postgres`/`redis` 全程不动。

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

# 规则：tag 版本号必须 > 已存在的版本，且没被占用。
# 注意：v0.1.137 已被官方 upstream 占用（合并带进来的），所以首发用 v0.1.138。
echo 0.1.138 > backend\cmd\server\VERSION
git add -A && git commit -m "feat: 自定义版本 0.1.138"
git push origin main
git tag v0.1.138
git push origin v0.1.138      # 若用 mine 远端：git push mine v0.1.138
```

`release.yml` 触发条件是 `push tags: v*`。它会：构建前端 → goreleaser 编译多架构二进制 →
产出 `sub2api_0.1.138_linux_amd64.tar.gz`（+ arm64）+ `checksums.txt` → 创建 **正式
Release**（`draft: false`）→ 推 GHCR 镜像 `ghcr.io/ly910914/sub2api:0.1.138`（amd64+arm64 manifest）→
回写 VERSION 到默认分支。

> ⚠️ **关于 pre-release**：`.goreleaser.yaml` 配的是 `prerelease: auto`——只要 tag **不带**
> `-rc`/`-beta` 之类后缀（即纯 `v0.1.138`），就是**正式版、非 pre-release**。
> 网页在线更新查的是 `/releases/latest`，**只返回正式、非预发布**的最新版，所以务必用纯版本号 tag。

### 3.4 ⭐ 把 GHCR 镜像包设为 Public（关键，否则服务器 pull 会 401）

GHCR 新推的包**默认是 Private**。服务器直接 `docker pull` 私有包会失败。二选一：

- **方案一（推荐，最省事）**：设为 Public。
  打开 `https://github.com/users/ly910914/packages/container/sub2api/settings`
  → `Danger Zone` → **Change visibility** → **Public**。
  之后任何机器都能匿名 `docker pull ghcr.io/ly910914/sub2api:0.1.138`。
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

1. **Release 页**：`github.com/ly910914/sub2api/releases/tag/v0.1.138` 应能看到资产：
   - `sub2api_0.1.138_linux_amd64.tar.gz`
   - `sub2api_0.1.138_linux_arm64.tar.gz`
   - `checksums.txt`
   - 且**不是 Draft、不是 Pre-release**。

2. **GHCR 镜像**（在任意能直连的机器上，本机或一台服务器）：
   ```bash
   docker manifest inspect ghcr.io/ly910914/sub2api:0.1.138 | grep -A2 architecture
   # 应看到 amd64（和 arm64）。能 inspect 成功 = 包可访问（Public 或已登录）。
   ```

3. **在线更新 API**（确认 latest 指向 0.1.138）：
   ```bash
   curl -s https://api.github.com/repos/ly910914/sub2api/releases/latest | grep '"tag_name"'
   # 期望： "tag_name": "v0.1.138"
   ```

只有这 3 项都 OK，才进入第 5 节去动线上服务器。

---

## 5. 首次平替：把每台服务器从官方镜像切到你的 GHCR 镜像

> 这是**一次性切换**（官方 `weishaw/sub2api:latest` → `ghcr.io/ly910914/sub2api:0.1.138`）。
> §5.1–5.5 先讲**单台手动**流程（便于理解每步在干什么），**§5.6 给一键脚本 `update.sh`**，第 6 节给**多台批量**。
> 强烈建议：**先在 1 台（灰度机）跑通，确认无误，再铺开到其余服务器。**

下面以实际部署目录 **`/docker`**（含 `docker-compose.local.yml` + `.env` + `data/` + `postgres_data/` + `redis_data/`，可能还有 `nginx/`）为例。

### 5.1 进入目录、加载 .env、记录回滚信息

```bash
ssh user@server1
cd /docker
set -a; . ./.env; set +a                          # 加载 .env 到当前 shell

docker compose -f docker-compose.local.yml ps
docker exec sub2api /app/sub2api --version        # 记录旧版本
docker inspect sub2api --format '{{.Image}}'      # 记录旧镜像 ID（回滚备用）
```

### 5.2 备份（大库**只备 settings**，原理见 §2.1）

```bash
backup_ts="$(date +%Y%m%d-%H%M%S)"; mkdir -p backups
PGU="${POSTGRES_USER:-sub2api}"; PGD="${POSTGRES_DB:-sub2api}"

# 轻量备份：只导出唯一会被删改的小表 settings（秒级、几 KB），再导出迁移清单备查
docker exec sub2api-postgres pg_dump -U "$PGU" -d "$PGD" -Fc -t settings -f "/tmp/s-${backup_ts}.dump"
docker cp "sub2api-postgres:/tmp/s-${backup_ts}.dump" "./backups/settings-${backup_ts}.dump"
docker exec sub2api-postgres rm -f "/tmp/s-${backup_ts}.dump" || true
docker exec sub2api-postgres psql -U "$PGU" -d "$PGD" -tA \
  -c "SELECT filename FROM schema_migrations ORDER BY filename;" > "backups/migrations-${backup_ts}.txt"

# 备份 compose（小文件）
cp docker-compose.local.yml "backups/docker-compose.local.yml.bak-${backup_ts}"
```

> 巨型库**不要**在这里全量 `pg_dump`——完整备份用卷快照/pgBackRest 独立做（见 §2.1）。
> 若你确实想要完整逻辑备份，把上面 `-t settings` 去掉即可（会很慢/很大）。

### 5.3 改 compose 的 image 指向你的 GHCR 镜像

```bash
# 把 sub2api 服务的 image 从官方换成你的 GHCR 固定版本 tag（只匹配含 sub2api 的那行）
sed -i -E 's#^([[:space:]]*image:[[:space:]]*).*sub2api.*#\1ghcr.io/ly910914/sub2api:0.1.138#' docker-compose.local.yml

grep -nF 'ghcr.io/ly910914/sub2api:0.1.138' docker-compose.local.yml
docker compose -f docker-compose.local.yml config >/dev/null && echo "compose OK"
```

> 建议**固定版本号 tag**（`:0.1.138`），不要用 `:latest`。固定 tag 才能精确控制每台版本、便于回滚。

### 5.4 只拉取并重建应用容器（绝不动 postgres/redis/nginx）

```bash
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
docker compose -f docker-compose.local.yml logs -f --tail=200 sub2api
```

> 用 `pull sub2api` + `up -d --no-deps sub2api`。**不要**用无范围的 `docker compose pull` /
> 全量 `up`，否则会顺带升级 `postgres:18-alpine` / `redis:8-alpine`。
> 日志里应能看到缺失迁移逐个 `applied`，最后监听 8080。若风控日志表很大，`147` 回填会让这步多等一会儿（见 §2.2）。

### 5.5 验证

```bash
docker compose -f docker-compose.local.yml ps
docker exec sub2api wget -q -T 3 -O- http://localhost:8080/health   # 容器内自检
docker exec sub2api /app/sub2api --version        # 应显示 0.1.138

# 核对 4 个迁移已执行（应返回 4 行）
docker exec -i sub2api-postgres psql -U "$PGU" -d "$PGD" -c \
"SELECT filename FROM schema_migrations
 WHERE filename IN ('145_admin_billing_records.sql','146_admin_billing_record_source.sql',
 '147_content_moderation_log_input_content.sql','151_remove_admin_compliance_acknowledgements.sql')
 ORDER BY filename;"
```

再登录网页确认：管理员账单页、风控页、图片对话页正常；合规确认弹窗已消失；用真实 API Key 走一笔小流量验证网关。

### 5.6 一键脚本 `update.sh`（把 5.1–5.5 合成一个，放到服务器 `/docker/update.sh`）

```bash
#!/usr/bin/env bash
# /docker/update.sh —— sub2api 一键更新 (单机, 在服务器本地运行)
# 用法:  bash update.sh [版本]                 例: bash update.sh 0.1.139
#        BACKUP_MODE=full bash update.sh 0.1.139   # 需要完整备份时(大库慎用)
#        BACKUP_MODE=none bash update.sh 0.1.139   # 已有独立全量备份/快照时
set -euo pipefail

# ===== 配置(按需改) =====
DEPLOY_DIR="${DEPLOY_DIR:-/docker}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.local.yml}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/ly910914/sub2api}"
DEFAULT_VERSION="0.1.138"
BACKUP_MODE="${BACKUP_MODE:-light}"   # light=只备settings(秒级) | full=完整dump | none=不备
# ========================

VER="${1:-$DEFAULT_VERSION}"; IMAGE="${IMAGE_REPO}:${VER}"
cd "$DEPLOY_DIR"
[ -f .env ] || { echo "找不到 $DEPLOY_DIR/.env"; exit 1; }
set -a; . ./.env; set +a
PGU="${POSTGRES_USER:-sub2api}"; PGD="${POSTGRES_DB:-sub2api}"
ts="$(date +%Y%m%d-%H%M%S)"; mkdir -p backups
echo ">>> 目标镜像: $IMAGE | 备份模式: $BACKUP_MODE"

# 1) 备份
cp "$COMPOSE_FILE" "backups/${COMPOSE_FILE}.bak-${ts}"
case "$BACKUP_MODE" in
  light)
    echo ">>> [备份] 仅 settings 表 + 迁移清单 (轻量, 秒级)"
    docker exec sub2api-postgres pg_dump -U "$PGU" -d "$PGD" -Fc -t settings -f "/tmp/s-${ts}.dump"
    docker cp "sub2api-postgres:/tmp/s-${ts}.dump" "backups/settings-${ts}.dump"
    docker exec sub2api-postgres rm -f "/tmp/s-${ts}.dump" || true
    docker exec sub2api-postgres psql -U "$PGU" -d "$PGD" -tA \
      -c "SELECT filename FROM schema_migrations ORDER BY filename;" > "backups/migrations-${ts}.txt" ;;
  full)
    echo ">>> [备份] 完整 pg_dump (大库会很慢/很大)"
    docker exec sub2api-postgres pg_dump -U "$PGU" -d "$PGD" -Fc -f "/tmp/f-${ts}.dump"
    docker cp "sub2api-postgres:/tmp/f-${ts}.dump" "backups/full-${ts}.dump"
    docker exec sub2api-postgres rm -f "/tmp/f-${ts}.dump" || true ;;
  none) echo ">>> [备份] 跳过" ;;
esac

# 2) 改 image + 校验 (只匹配含 sub2api 的那行)
sed -i -E "s#^([[:space:]]*image:[[:space:]]*).*sub2api.*#\1${IMAGE}#" "$COMPOSE_FILE"
grep -nF "$IMAGE" "$COMPOSE_FILE" >/dev/null || { echo "image 替换失败"; exit 1; }
docker compose -f "$COMPOSE_FILE" config >/dev/null

# 3) 只拉应用 + 只重建应用容器 (绝不动 postgres/redis/nginx)
docker compose -f "$COMPOSE_FILE" pull sub2api
docker compose -f "$COMPOSE_FILE" up -d --no-deps sub2api

# 4) 等健康 (容器内自检, 不受宿主端口映射影响)
echo -n ">>> 等待健康"; ok=0
for i in $(seq 1 45); do
  if docker exec sub2api wget -q -T 3 -O /dev/null http://localhost:8080/health 2>/dev/null; then ok=1; echo " OK"; break; fi
  echo -n "."; sleep 2
done
[ "$ok" = 1 ] || { echo " 失败!"; docker compose -f "$COMPOSE_FILE" logs --tail=100 sub2api; exit 1; }

# 5) 验证
echo ">>> 版本: $(docker exec sub2api /app/sub2api --version)"
echo ">>> 迁移(期望含145/146/147/151):"
docker exec -i sub2api-postgres psql -U "$PGU" -d "$PGD" -tA -c \
"SELECT filename FROM schema_migrations WHERE filename IN ('145_admin_billing_records.sql','146_admin_billing_record_source.sql','147_content_moderation_log_input_content.sql','151_remove_admin_compliance_acknowledgements.sql') ORDER BY filename;"
echo ">>> 完成: $IMAGE"
```

**用法（在你现在这台 `/docker`）：**

```bash
cd /docker
vi update.sh                  # 粘贴上面内容
bash update.sh 0.1.138        # 轻量备份 + 更新到 0.1.138
# 以后:  bash update.sh 0.1.139
```

**灰度机这一台用 `update.sh` 跑通、验证无误后，再进入第 6 节铺开到其余服务器。**

---

## 6. 多台服务器批量更新（标准化脚本）

> 控制端可以是你本机的 **Git Bash / WSL**，或任意一台能 SSH 到所有服务器的 Linux「跳板机」。

### 6.0 前置条件 + 脚本清单（先看这里）

**思路**：多台 = 把 §5.6 的 `update.sh` 分发到每台、逐台执行。所以只需两个文件：`update.sh`（§5.6，干活）+ `fleet-run.sh`（§6.3，分发并逐台调用）。

**前置条件：**

1. **SSH 免密**：控制端能用 SSH key 免密登录每台服务器（`ssh-copy-id user@host` 配好）。
2. **目录结构一致**：每台部署目录都是 `/docker`（含 `docker-compose.local.yml` + `.env` + 数据目录）。
   个别机器目录不同，可在 `fleet-run.sh` 里用 `DEPLOY_DIR=...` 覆盖，或直接改那台 `update.sh` 顶部。
3. **⭐ 换行符必须是 LF，不能是 CRLF**：你在 Windows 上创建的 `.sh` / `servers.txt` 默认是
   **CRLF（`\r\n`）**，传到 Linux 后 `while read` 会把 `\r` 读进变量、heredoc 也会错乱，**直接报莫名其妙的错**。
   解决其一：用 VS Code 右下角把行尾改成 **LF**；或在 Linux 上 `sed -i 's/\r$//' update.sh fleet-run.sh servers.txt`。
4. **私有 GHCR 要先登录**：若你**没**按 §3.4 把 GHCR 包设为 Public，则 `update.sh` 里的 `pull` 会 `401`。
   先给每台登录一次（一次性）：
   ```bash
   # 控制端，对清单每台执行（PAT 勾 read:packages）
   while read -r t _; do [[ -z "$t" || "$t" == \#* ]] && continue;
     ssh "$t" "echo '<你的PAT>' | docker login ghcr.io -u ly910914 --password-stdin"; done < servers.txt
   ```
   设了 Public 就**跳过这步**。
5. **滚动停机意识**：`up -d --no-deps sub2api` 重建应用容器有**秒级停机**；大库还要叠加 `147` 回填耗时（§2.2）。
   `fleet-run.sh` 是**逐台串行**（不是并发），天然就是滚动更新；务必先按 §6.4 灰度 1 台。

**本文用到的文件（控制端，和 `servers.txt`、`update.sh` 放同一目录）：**

| 文件 | 作用 | 出处 |
| --- | --- | --- |
| `servers.txt` | 服务器清单（你手建，每行一个 `user@host`） | 6.1 |
| `update.sh` | 单机一键更新（备份+切镜像+重建+等health+验证） | 5.6 |
| `fleet-run.sh` | 把 `update.sh` 分发到每台并逐台执行 | 6.3 |

> 仓库里 `deploy/build_image.sh`（已提交）可在你想**本机 build** 而非用 CI/GHCR 时备用。
> 数据库迁移由应用启动自动跑，**无需手工执行 SQL**；如需在单台精细核对，用下面这段**内联校验 SQL**即可：

```bash
# 单台更新后精细核对（只读，无副作用）。先 ssh 进该服务器：
ssh user@serverX
cd /docker && set -a; . ./.env; set +a
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

每行一个 SSH 目标。`#` 开头为注释，空行忽略。

```text
# 每行一台 user@host
user@server1.example.com
user@server2.example.com
# user@10.0.0.13
```

### 6.2 准备 `update.sh`

`update.sh` 就是 §5.6 那个脚本，放在控制端当前目录即可（`fleet-run.sh` 会自动 `scp` 到每台）。
确认它默认 `DEPLOY_DIR=/docker`、`IMAGE_REPO=ghcr.io/ly910914/sub2api` 与你的实际一致。

### 6.3 分发并执行脚本 `fleet-run.sh`

```bash
#!/usr/bin/env bash
# fleet-run.sh —— 把 update.sh 分发到每台并逐台执行(滚动更新)。
# 用法:  bash fleet-run.sh [版本] [servers.txt]   例: bash fleet-run.sh 0.1.139
# 环境:  REMOTE_DIR  远端存放/执行 update.sh 的目录 (默认 /docker)
#        BACKUP_MODE 透传给 update.sh (light|full|none, 默认 light)
set -euo pipefail
VER="${1:-0.1.138}"
SERVERS_FILE="${2:-servers.txt}"
REMOTE_DIR="${REMOTE_DIR:-/docker}"
BACKUP_MODE="${BACKUP_MODE:-light}"
[ -f update.sh ]      || { echo "当前目录缺少 update.sh (见 §5.6)"; exit 1; }
[ -f "$SERVERS_FILE" ] || { echo "找不到清单文件: $SERVERS_FILE"; exit 1; }

fail=0
while IFS= read -r host || [ -n "$host" ]; do
  host="${host%$'\r'}"                                   # 容错 CRLF
  [[ -z "${host// }" || "$host" == \#* ]] && continue
  echo "==================== [$host] → $VER ===================="
  scp -q update.sh "$host:${REMOTE_DIR}/update.sh"
  if ssh "$host" "BACKUP_MODE='$BACKUP_MODE' bash ${REMOTE_DIR}/update.sh '$VER'"; then
    echo "==== [$host] OK ===="
  else
    echo "==== [$host] FAIL (见上方日志) ====" >&2
    fail=1
  fi
  echo
done < "$SERVERS_FILE"

[ "$fail" = 0 ] && echo "==== 全部完成 ====" || { echo "==== 有服务器失败，请检查/回滚 ===="; exit 1; }
```

`update.sh` 内部已自带「备份 → 切镜像 → 重建 → 等 health → 验证版本/迁移」，所以
`fleet-run.sh` 跑完即等于**每台都已备份、更新并自检**，无需再单独跑验证脚本。

### 6.4 推荐执行顺序（灰度 → 全量）

```bash
chmod +x update.sh fleet-run.sh

# 1) 灰度：清单里先只放 1 台
printf 'user@server1.example.com\n' > servers.canary.txt
bash fleet-run.sh 0.1.138 servers.canary.txt
#    → 人工登录这台网页确认业务正常（尤其大库观察启动是否被 147 回填拖久）

# 2) 全量铺开（其余服务器）
bash fleet-run.sh 0.1.138 servers.txt
```

> ⚠️ 切镜像用的 `sed` 假设 compose 中**只有一行** `image:` 含 `sub2api`（应用服务那行）。
> `postgres:18-alpine` / `redis:8-alpine` 不含 `sub2api`，不会被误改；注释 `# image:` 也不匹配。
> `update.sh` 里 `grep` + `config` 两步会当场暴露改错。

---

## 7. 日常更新的两种节奏

### 7.1 节奏 A（推荐 · 正式定版 · 多台统一）

以后每次改完代码（当前已发布到 `0.1.138`，下一次就是 `0.1.139`）：

```powershell
# 本机
cd D:\work\code\my\sub2api
git add -A && git commit -m "feat: xxx 修改"
echo 0.1.139 > backend\cmd\server\VERSION    # 版本号递增
git add backend/cmd/server/VERSION && git commit -m "chore: VERSION 0.1.139"
git push origin main
git tag v0.1.139 && git push origin v0.1.139  # 触发 CI
```

CI 跑完后，控制端一行铺开所有服务器（`update.sh` + `fleet-run.sh` 见 §5.6 / §6.3）：

```bash
bash fleet-run.sh 0.1.139 servers.txt
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
> `ssh user@serverX 'cd /docker && docker compose -f docker-compose.local.yml restart sub2api'`

**重要特性与边界：**

- **热更是临时的**：它改的是容器**可写层**。之后任何 `docker compose up -d --force-recreate`
  或改 `image` 重建容器，可写层被丢弃 → **回退到镜像内置版本**。所以正式定版仍要走节奏 A，让镜像 tag 追上。
- **不需要 Docker Hub / GHCR**：点击更新只读 GitHub Release 归档。镜像仓库只服务于节奏 A 的 `docker pull`。
- **多台不建议逐台点**：N 台都想升时，直接走节奏 A（一次打 tag、批量 `pull`）比逐台点网页省事得多。
- **回滚**：见 §8.3（最简单是镜像回滚，重建容器即回到镜像内置版本）。

---

## 8. 回滚

### 8.1 镜像回滚（首选，最快）—— 单台或批量

把镜像换回**上一版**（如从 `0.1.139` 回到 `0.1.138`），重跑脚本即可：

```bash
# 批量回滚整队到上一版（用 update.sh, 默认 light 备份, 想跳过备份加 BACKUP_MODE=none）
BACKUP_MODE=none bash fleet-run.sh 0.1.138 servers.txt

# 单台（在该服务器 /docker 下）
BACKUP_MODE=none bash /docker/update.sh 0.1.138
```

> **首次平替后想退回官方镜像**：把 image 改回 `weishaw/sub2api:latest` 再 `pull`+`up`（即 §5.3 反向）：
> ```bash
> cd /docker
> sed -i -E 's#^([[:space:]]*image:[[:space:]]*).*sub2api.*#\1weishaw/sub2api:latest#' docker-compose.local.yml
> docker compose -f docker-compose.local.yml pull sub2api
> docker compose -f docker-compose.local.yml up -d --no-deps sub2api
> ```

- `145`/`146`/`147` 是纯加表/加列，旧二进制会忽略 → **镜像回滚通常不需要恢复数据库**。
- `151` 删了 `settings` 的合规确认行，回退旧二进制后最多再弹一次合规确认弹窗（不是丢业务数据）。

### 8.2 数据库回滚（仅当迁移本身出问题时，极少需要）

按你做过的备份方式恢复：

```bash
ssh user@serverX
cd /docker; set -a; . ./.env; set +a
PGU="${POSTGRES_USER:-sub2api}"; PGD="${POSTGRES_DB:-sub2api}"
docker compose -f docker-compose.local.yml stop sub2api

# (a) light 备份(只备了 settings)：恢复 settings 表
docker cp "./backups/settings-<ts>.dump" "sub2api-postgres:/tmp/s.dump"
docker exec sub2api-postgres pg_restore -U "$PGU" -d "$PGD" --clean --if-exists -t settings "/tmp/s.dump"

# (b) full 备份：完整恢复
# docker cp "./backups/full-<ts>.dump" "sub2api-postgres:/tmp/f.dump"
# docker exec sub2api-postgres pg_restore -U "$PGU" -d "$PGD" --clean --if-exists "/tmp/f.dump"

# (c) 巨型库：用卷快照 / pgBackRest PITR 恢复（见 §2.1），不靠 pg_restore

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
  1. 每次发布**版本号必须递增**（如 `0.1.138 → 0.1.139`），否则网页不认为有更新、镜像 tag 也会撞。
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

**Q8. 不同服务器端口/目录不同怎么办？**
health 检查在容器内执行（`docker exec sub2api wget ... localhost:8080`），不受宿主端口影响。
部署目录不同就在 `fleet-run.sh` 用 `REMOTE_DIR=...` 覆盖，或改那台 `update.sh` 顶部的 `DEPLOY_DIR`。

**Q9. 脚本在 Linux 上报 `$'\r': command not found` / `No such file or directory`？**
你的 `.sh` 或 `servers.txt` 是 Windows 的 CRLF 换行。按 §6.0 第 3 条转成 LF（`sed -i 's/\r$//' update.sh fleet-run.sh servers.txt`）。

**Q10. 每台 `backups/` 会不会越堆越多撑爆磁盘？**
`light` 模式每份只有几 KB（settings + 迁移清单），基本无压力；`full` 模式才大。定期清理：
```bash
ssh user@serverX 'cd /docker/backups && ls -1t settings-*.dump full-*.dump 2>/dev/null | tail -n +15 | xargs -r rm -f'
```

**Q11. 多台在负载均衡后面，更新时会不会整体抖一下？**
`fleet-run.sh` 是**逐台串行**，每台只在自己 `up -d --no-deps sub2api` 那几秒不可用（health 通过才轮下一台），
相当于滚动更新。务必先灰度 1 台（§6.4）。`postgres`/`redis` 全程不动。大库注意 `147` 回填会拉长单台耗时（§2.2）。

**Q12. 需要手工执行 SQL 迁移吗？**
**不需要**。4 个迁移由应用启动时自动跑（按 `schema_migrations` 补缺）。
只想**只读核对**结果时，用 §6.0 那段内联校验 SQL 即可，无副作用。

**Q13. 大库每次更新都要全量 `pg_dump` 吗？**
**不要。** 应用更新不碰 `postgres_data`；唯一删数据的是 `151`（删 `settings` 几行）。
所以默认 `BACKUP_MODE=light` 只备 `settings`（秒级）即可。完整备份用卷快照/pgBackRest 独立做（见 §2.1）。

---

## 11. 速查卡（贴墙）

```bash
# —— 本机发版（每次, 版本号递增）——
git add -A && git commit -m "..."
echo 0.1.x > backend/cmd/server/VERSION && git commit -am "VERSION 0.1.x"
git push origin main
git tag v0.1.x && git push origin v0.1.x        # CI 自动出 GHCR 镜像 + Release

# —— 控制端批量铺开（update.sh=§5.6, fleet-run.sh=§6.3）——
bash fleet-run.sh 0.1.x servers.txt

# —— 灰度先 1 台 ——
printf 'user@server1\n' > servers.canary.txt && bash fleet-run.sh 0.1.x servers.canary.txt

# —— 回滚整队到上一版 ——
BACKUP_MODE=none bash fleet-run.sh 0.1.(x-1) servers.txt

# —— 单台一键（在服务器 /docker）——
bash /docker/update.sh 0.1.x
```

---

## 附录 A. 完整脚本清单

### A.1 脚本清单（复制即用）

| 脚本 | 作用 | 章节 |
| --- | --- | --- |
| `update.sh`（放服务器 `/docker`） | 单机一键：备份(light) + 切镜像 + 只重建应用 + 等 health + 验证版本/迁移 | 5.6 |
| `servers.txt`（控制端） | 服务器清单（手建，每行一个 `user@host`） | 6.1 |
| `fleet-run.sh`（控制端） | 把 `update.sh` 分发到每台并逐台执行（滚动更新） | 6.3 |
| 私有 GHCR 一次性登录 | 仅当未设 Public 时，给每台 `docker login ghcr.io` | 6.0 |
| 灰度→全量执行顺序 | 先 1 台 canary，再全量 | 6.4 |

### A.2 仓库内可选脚本 + 内联校验

| 项 | 作用 | 说明 |
| --- | --- | --- |
| `deploy/build_image.sh`（已提交） | 本机构建镜像 | 仅在想本机 build 而非用 CI/GHCR 时备用 |
| 内联校验 SQL（见 §6.0） | 更新后只读核对 4 迁移/新表/新列/合规清空 | 只读、无副作用，无需外部 SQL 文件 |

### A.3 单台手动命令（散落各节）

| 操作 | 命令要点 | 章节 |
| --- | --- | --- |
| 发版 | `git tag v0.1.x && git push origin v0.1.x` | 3.3 / 7.1 |
| 备份（轻量） | `pg_dump -Fc -t settings`（只备会被删的小表） | 2.1 / 5.2 |
| 切镜像 | `sed` 改 `image:` → `ghcr.io/ly910914/sub2api:0.1.x` | 5.3 |
| 只更应用 | `pull sub2api` + `up -d --no-deps sub2api` | 5.4 |
| 验证 | 容器内 `/health` + `--version` + 查 `schema_migrations` | 5.5 |
| 网页热更 | 网页点「检查更新」→「立即更新」→「重启」（全程网页自助） | 7.2 |
| 回滚镜像 | `BACKUP_MODE=none bash fleet-run.sh <上一版>` | 8.1 |
| 回滚数据库 | light→`pg_restore -t settings`；full→完整；大库→快照/PITR | 8.2 |

---

## 附录 B. 完整注意事项清单（自查表）

| # | 注意事项 | 章节 |
| --- | --- | --- |
| 0 | **不需要 Docker Hub**：点击更新只用 GitHub Release 归档；多台 pull 用 GHCR | §1.2 / §3.2 / Q4 |
| 1 | **两种动态更新别混**：镜像更新（永久）vs 网页热更（临时） | §1 |
| 2 | 网页热更是临时的，`--force-recreate`/改 image 重建即**回退到镜像内置版本** | §1.1 / §7.2 |
| 3 | 网页热更后需**重启进程**才生效：网页点「重启」(`os.Exit`→Docker 自动拉起) 或 `docker restart sub2api` | §1.1 / §7.2 |
| 4 | `151` 会 `DELETE` `settings` 行 → 更新前备份；**大库只需备 `settings`**（`light` 模式，秒级，无需全量） | §2.1 / §5.2 |
| 5 | ⭐ **GHCR 包默认 Private，要设 Public**，否则服务器 `pull` 报 401 | §3.4 / Q1 |
| 6 | tag 用**纯版本号**（`v0.1.x`），别带 `-rc`/`-beta`（否则被当 pre-release，`/releases/latest` 取不到） | §3.3 / §9 |
| 7 | **只 `pull sub2api`**，绝不用无范围 `docker compose pull`（会升级 pg/redis） | §5.4 / §6.3 / Q5 |
| 8 | 用**固定版本 tag**（`:0.1.x`），别用 `:latest`，便于精确控制与回滚 | §5.3 |
| 9 | **版本号每次必须递增**，否则网页不提示更新、镜像 tag 撞车 | §9 / Q2 |
| 10 | **绝不修改已跑过的旧迁移文件**（checksum 不符会拒绝启动）；改库永远新增更大编号文件 | §9 / Q2 |
| 11 | ⭐ **CRLF→LF 换行符**：Windows 建的 `.sh`/`servers.txt` 要转成 LF | §6.0 / Q9 |
| 12 | 私有 GHCR 时，fleet 脚本前需先给每台 `docker login ghcr.io` | §6.0 |
| 13 | 每台 `up -d --no-deps sub2api` 有**秒级停机**，逐台串行=滚动更新，先灰度 1 台 | §6.0 / §6.4 / Q11 |
| 14 | `update.sh` 切镜像的 `sed` 假设 compose 中**只有一行** `image:` 含 `sub2api` | §5.6 / §6.4 |
| 15 | 各台 `backups/` 会堆积，**定期清理**旧备份 | Q10 |
| 16 | 直连 GitHub 偶发抖动可配 `UPDATE_PROXY_URL`（网页热更）/ 重试 `pull`（镜像） | §7.2 / Q7 |
| 17 | 端口在容器内自检自动适配；目录不同用 `REMOTE_DIR`/`DEPLOY_DIR` 覆盖 | Q8 |
| 18 | 多台数据独立（各自 `postgres_data`/`redis_data`/`data`），批量互不影响 | Q6 |
| 19 | **大库不必每次全量 `pg_dump`**：默认 `light` 只备 `settings`；完整备份用卷快照/pgBackRest | §2.1 / Q13 |
| 20 | ⚠️ 大库 `147` 回填会拉长首启（期间应用不可用）→ 低峰期 + 先灰度观察 | §2.2 |

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
