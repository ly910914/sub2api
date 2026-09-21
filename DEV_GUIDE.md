# sub2api 项目开发指南

> 本文档记录项目环境配置、常见坑点和注意事项，供 Claude Code 和团队成员参考。

## 一、项目基本信息

| 项目 | 说明 |
|------|------|
| **上游仓库** | Wei-Shaw/sub2api |
| **Fork 仓库** | bayma888/sub2api-bmai |
| **技术栈** | Go 后端 (Ent ORM + Gin) + Vue3 前端 (pnpm) |
| **数据库** | PostgreSQL 16 + Redis |
| **包管理** | 后端: go modules, 前端: **pnpm**（不是 npm） |

## 二、本地环境配置

### PostgreSQL 16 (Windows 服务)

| 配置项 | 值 |
|--------|-----|
| 端口 | 5432 |
| psql 路径 | `C:\Program Files\PostgreSQL\16\bin\psql.exe` |
| pg_hba.conf | `C:\Program Files\PostgreSQL\16\data\pg_hba.conf` |
| 数据库凭据 | user=`sub2api`, password=`sub2api`, dbname=`sub2api` |
| 超级用户 | user=`postgres`, password=`postgres` |

### Redis

| 配置项 | 值 |
|--------|-----|
| 端口 | 6379 |
| 密码 | 无 |

### 开发工具

```bash
# golangci-lint（CI 用 v2.13，本地建议装同一版以免版本差异带来的噪音）
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13

# pnpm (前端包管理)
npm install -g pnpm
```

## 三、CI/CD 流水线

### GitHub Actions Workflows

| Workflow | 触发条件 | 检查内容 |
|----------|----------|----------|
| **backend-ci.yml** | push, pull_request | 单元测试 + 集成测试 + golangci-lint v2.13 |
| **security-scan.yml** | push, pull_request, 每周一 | govulncheck + gosec + pnpm audit |
| **release.yml** | tag `v*` | 构建发布（PR 不触发） |

### CI 要求

- Go 版本必须是 **1.27.0**：三个 workflow 都用 `go-version-file: backend/go.mod` 取版本，随后硬断言 `go version | grep -q 'go1.27.0'`。升级 Go 时要同时改 `backend/go.mod`、`backend-ci.yml`（两处）、`release.yml`、`security-scan.yml` 里的这句断言，**以及三个 Dockerfile 里的 Go 构建镜像**（`Dockerfile` / `deploy/Dockerfile` 的 `ARG GOLANG_IMAGE`、`backend/Dockerfile` 的 `FROM golang:`）。前者漏了 CI 会在版本校验步骤直接失败；**后者漏了 CI 不会报，而是等到有人用这些 Dockerfile 构建时才失败**（`go.mod requires go >= X (running Y; GOTOOLCHAIN=local)`）。
- 前端使用 `pnpm install --frozen-lockfile`，必须提交 `pnpm-lock.yaml`

### 本地测试命令

```bash
# 后端单元测试
cd backend && go test -tags=unit ./...

# 后端集成测试
cd backend && go test -tags=integration ./...

# 代码质量检查
cd backend && golangci-lint run ./...

# 前端依赖安装（必须用 pnpm）
cd frontend && pnpm install
```

## 四、常见坑点 & 解决方案

### 坑 1：pnpm-lock.yaml 必须同步提交

**问题**：`package.json` 新增依赖后，CI 的 `pnpm install --frozen-lockfile` 失败。

**原因**：上游 CI 使用 pnpm，lock 文件不同步会报错。

**解决**：
```bash
cd frontend
pnpm install  # 更新 pnpm-lock.yaml
git add pnpm-lock.yaml
git commit -m "chore: update pnpm-lock.yaml"
```

---

### 坑 2：npm 和 pnpm 的 node_modules 冲突

**问题**：之前用 npm 装过 `node_modules`，pnpm install 报 `EPERM` 错误。

**解决**：
```bash
cd frontend
rm -rf node_modules  # 或 PowerShell: Remove-Item -Recurse -Force node_modules
pnpm install
```

---

### 坑 3：PowerShell 中 bcrypt hash 的 `$` 被转义

**问题**：bcrypt hash 格式如 `$2a$10$xxx...`，PowerShell 把 `$2a` 当变量解析，导致数据丢失。

**解决**：将 SQL 写入文件，用 `psql -f` 执行：
```bash
# 错误示范（PowerShell 会吃掉 $）
psql -c "INSERT INTO users ... VALUES ('$2a$10$...')"

# 正确做法
echo "INSERT INTO users ... VALUES ('\$2a\$10\$...')" > temp.sql
psql -U sub2api -h 127.0.0.1 -d sub2api -f temp.sql
```

---

### 坑 4：psql 不支持中文路径

**问题**：`psql -f "D:\中文路径\file.sql"` 报错找不到文件。

**解决**：复制到纯英文路径再执行：
```bash
cp "D:\中文路径\file.sql" "C:\temp.sql"
psql -f "C:\temp.sql"
```

---

### 坑 5：PostgreSQL 密码重置流程

**场景**：忘记 PostgreSQL 密码。

**步骤**：
1. 修改 `C:\Program Files\PostgreSQL\16\data\pg_hba.conf`
   ```
   # 将 scram-sha-256 改为 trust
   host    all    all    127.0.0.1/32    trust
   ```
2. 重启 PostgreSQL 服务
   ```powershell
   Restart-Service postgresql-x64-16
   ```
3. 无密码登录并重置
   ```bash
   psql -U postgres -h 127.0.0.1
   ALTER USER sub2api WITH PASSWORD 'sub2api';
   ALTER USER postgres WITH PASSWORD 'postgres';
   ```
4. 改回 `scram-sha-256` 并重启

---

### 坑 6：Go interface 新增方法后 test stub 必须补全

**问题**：给 interface 新增方法后，编译报错 `does not implement interface (missing method XXX)`。

**原因**：所有测试文件中实现该 interface 的 stub/mock 都必须补上新方法。

**解决**：
```bash
# 搜索所有实现该 interface 的 struct
cd backend
grep -r "type.*Stub.*struct" internal/
grep -r "type.*Mock.*struct" internal/

# 逐一补全新方法
```

---

### 坑 7：Windows 上 psql 连 localhost 的 IPv6 问题

**问题**：psql 连 `localhost` 先尝试 IPv6 (::1)，可能报错后再回退 IPv4。

**建议**：直接用 `127.0.0.1` 代替 `localhost`。

---

### 坑 8：Windows 没有 make 命令

**问题**：CI 里用 `make test-unit`，本地 Windows 没有 make。

**解决**：直接用 Makefile 里的原始命令：
```bash
# 代替 make test-unit
go test -tags=unit ./...

# 代替 make test-integration
go test -tags=integration ./...
```

---

### 坑 9：Ent Schema 修改后必须重新生成

**问题**：修改 `ent/schema/*.go` 后，代码不生效。

**解决**：
```bash
cd backend
go generate ./ent  # 重新生成 ent 代码（json.RawMessage 字段会生成为同类型的 jsontext.Value，属预期）
git add ent/       # 生成的文件也要提交
```

---

### 坑 10：前端测试看似正常，但后端调用失败（模型映射被批量误改）

**典型现象**：
- 前端按钮点测看起来正常；
- 实际通过 API/客户端调用时返回 `Service temporarily unavailable` 或提示无可用账号；
- 常见于 OpenAI 账号（例如 Codex 模型）在批量修改后突然不可用。

**根因**：
- OpenAI 账号编辑页默认不显式展示映射规则，容易让人误以为“没映射也没关系”；
- 但在**批量修改同时选中不同平台账号**（OpenAI + Antigravity/Gemini）时，模型白名单/映射可能被跨平台策略覆盖；
- 结果是 OpenAI 账号的关键模型映射丢失或被改坏，后端选不到可用账号。

**修复方案（按优先级）**：
1. **快速修复（推荐）**：在批量修改中补回正确的透传映射（例如 `gpt-5.3-codex -> gpt-5.3-codex-spark`）。
2. **彻底重建**：删除并重新添加全部相关账号（最稳但成本高）。

**关键经验**：
- 如果某模型已被软件内置默认映射覆盖，通常不需要额外再加透传；
- 但当上游模型更新快于本仓库默认映射时，**手动批量添加透传映射**是最简单、最低风险的临时兜底方案；
- 批量操作前尽量按平台分组，不要混选不同平台账号。

---

### 坑 11：PR 提交前检查清单

提交 PR 前务必本地验证：

- [ ] `go test -tags=unit ./...` 通过
- [ ] `go test -tags=integration ./...` 通过
- [ ] `golangci-lint run ./...` 无新增问题
- [ ] `pnpm-lock.yaml` 已同步（如果改了 package.json）
- [ ] 所有 test stub 补全新接口方法（如果改了 interface）
- [ ] Ent 生成的代码已提交（如果改了 schema）

## 五、合并上游官方代码（安全合并 / 保功能）

> 从上游拉取并合并官方代码的标准流程，目标只有两个：**绝不丢失任何自定义功能**，且**绝不复活已删除的功能**。
> 既可照着手动执行，也可把本节整段丢给 Claude / Codex，让它按此执行。

### 5.0 仓库背景与合并方式

| 项 | 值 |
|---|---|
| `origin` | 我们的 fork（push 目标） |
| `upstream` | `Wei-Shaw/sub2api`（官方，只读上游来源） |
| 合并方式 | **merge commit**（`git merge upstream/main`），沿用现有历史 —— 不用 rebase |
| 当前版本 | `backend/cmd/server/VERSION`（如 `0.1.140`） |

> 备份分支命名沿用 `backup/pre-merge-*`（已存在 `backup/pre-merge-v0.1.138-upstream-sync`）。

### 5.1 必须在每次合并后存活的自定义功能

1. **账单统计 / Manual Billing** —— 全栈。前端 `BillingView.vue` + 账单 API/图表；后端 `admin_billing*`（Ent 实体、handler、service、repo、model）+ 迁移。
2. **图片对话 / 生图 / Image Chat** —— 仅前端。`ImageChatView.vue`、`images.ts`、`imageConversations` store、`ImageLightbox`/`LazyImage`、`imageGeneration` 功能开关。
3. **风控增强 / Risk Control** —— 后端 config + 分组/用户弹窗调整。
4. **移除合规门禁 / Compliance gate 已删除** —— 前后端合规模块都已删除，必须**保持删除**。
5. **发布/更新源指向自有仓库** —— `deploy/*`、根 `Dockerfile*`、`VERSION`、`update_service.go`、README，永远保我们的。

### 5.2 核心原则：把"以我为主"和"正确合并"统一起来

解冲突时**永远先判断文件属于哪个"桶"**，绝不无脑取某一整边：

- **我独有的文件**（自定义 view/组件/store/api/后端 service）→ **保我的**。
- **共享集成文件**（router、i18n、sidebar、types、store/api 的 `index`、后端 config/routes）→ **UNION 合并**：我的注册/路由/键 **和** 上游新增的两边都留。这里"以我为主"= 我的部分不可丢，**不是**丢掉上游。
- **我删掉的文件**（合规）→ **保持删除**。
- **发布/部署/版本/更新源** → **保我的**。
- **其它一切**（上游在我们没碰过的地方的修复/新功能）→ **取上游**（这就是"正确合并"的另一半）。
- **生成代码**（Ent `backend/ent/*`、Wire `*wire_gen.go`/`wire.go`）→ **不手动合**，解好源头（Ent schema、Wire provider）后**重新生成**。

> **不能删功能**对两边都成立：既保我们的，也保上游的。唯一刻意缺席的只有合规模块。

### 5.3 文件分桶清单

> 清单随版本变化，可用合并基点动态重建：
> ```bash
> BASE=$(git merge-base HEAD upstream/main)
> git diff --diff-filter=A --name-only $BASE..HEAD   # 我新增（OURS）
> git diff --diff-filter=D --name-only $BASE..HEAD   # 我删除（DELETED）
> git diff --diff-filter=M --name-only $BASE..HEAD   # 我修改（UNION 候选）
> ```

#### OURS —— fork 独有新文件（保我的，`git checkout --ours`）

前端：`views/admin/BillingView.vue`、`views/user/ImageChatView.vue`、`api/admin/billing.ts`、`api/images.ts`、`stores/imageConversations.ts`、`components/charts/BillingTrendChart.vue`、`components/common/ImageLightbox.vue`、`LazyImage.vue`、`utils/imageKeys.ts` 及其 `__tests__`。

后端（Manual Billing）：`ent/schema/admin_billing_record.go`（Ent 实体源头）、`internal/handler/admin/admin_billing_handler.go`、`internal/service/admin_billing_service.go`、`internal/repository/admin_billing_repo.go`、`internal/model/admin_billing.go`、`ent/adminbillingrecord*`（生成代码，见下 GENERATED）。

部署文档：`deploy/MULTI_SERVER_GHCR_UPDATE_CN.md`、`deploy/docker-compose.infra.yml`。

#### DELETED —— 合规门禁已移除（保持删除，`git rm`）

冲突时选删除：`frontend/src/api/admin/compliance.ts`、`components/admin/AdminComplianceDialog.vue`、`stores/adminCompliance.ts`、`backend/internal/handler/admin/compliance_handler.go`、`internal/service/admin_compliance*.go`、`internal/server/middleware/admin_compliance*.go`、`docs/legal/admin-compliance.{en,zh}.md`。同时下方 UNION 文件里不要重新注册合规。

#### UNION —— 共享文件，手动合并，两边都保留

前端：`router/index.ts`（保 `/admin/billing`=`AdminBilling`、`/image`=`ImageChat` 及 `requiresImageGeneration` 守卫）、`i18n/locales/{zh,en}.ts`（保 `nav.imageGeneration`、`images:{}`、`admin.billing:{}` + 上游所有键）、`components/layout/AppSidebar.vue`（保两条导航 + 图标 + `flagImageGeneration`）、`utils/featureFlags.ts`（保 `imageGeneration` 开关）、`types/index.ts`、`router/meta.d.ts`、`stores/index.ts`（合规保持未注册）、`api/admin/index.ts`（保 `billing` 导出、不导出合规）、`stores/app.ts`、`App.vue`、`AppHeader.vue`、`views/admin/SettingsView.vue`（合规区块保持移除）、`LegalDocumentView.vue`、`views/user/DashboardView.vue`、`components/user/dashboard/UserDashboardStats.vue`、`GroupBadge.vue`、`GroupOptionItem.vue`、`components/admin/group/Group{RPMOverrides,RateMultipliers}Modal.vue`、`components/admin/user/UserAllowedGroupsModal.vue`、`views/admin/GroupsView.vue`、`HomeView.vue`、`KeyUsageView.vue`、`api/admin/settings.ts`、`api/client.ts`。

后端：`internal/config/config.go`（保风控/账单配置）、`internal/server/routes/{admin,payment}.go`（保账单路由、无合规路由）、`internal/handler/{handler,wire,setting_handler,page_handler}.go`、`handler/admin/setting_handler.go`、`handler/dto/settings.go`、`internal/service/{setting_service,settings_view,wire,domain_constants}.go`、`internal/repository/wire.go`、`internal/setup/setup.go`、`internal/service/update_service.go`（偏 OURS：更新源指向 fork）。

#### OURS（发布/部署/版本/更新源）—— 永远保 fork 指向

根 `Dockerfile`、`Dockerfile.goreleaser`、`backend/cmd/server/VERSION`、`deploy/*`（`Dockerfile`、`docker-compose*.yml`、`install.sh`、`docker-deploy.sh`、`sub2api.service`、`config.example.yaml`、`DOCKER.md`、`README.md`）、根 `README.md`/`README_JA.md`、`update_service.go`/`domain_constants.go`。

#### GENERATED —— 重新生成，不手动合

先取一边得到能解析的文件，再重生成：Ent（`ent/client.go`、`ent.go`、`mutation.go`、`migrate/schema.go`、`hook/`、`intercept/`、`predicate/`、`runtime/`、`tx.go`、`adminbillingrecord*`）→ `cd backend && go generate ./ent`；Wire（`cmd/server/wire_gen.go` 及各 `wire.go`）→ `cd backend && go generate ./...`；`go.sum` 交给 build/`go mod tidy`，不手改冲突标记。重生成后 `go build ./...` 必须过。（参见坑 9）

#### MIGRATIONS —— 两边都留，撞号改我的

`backend/migrations/NNN_*.sql`。号已和上游重叠（如两个 `147_*`、三个 `151_*`）。冲突时两个文件都留；若我方迁移和**不同内容**的上游同号迁移撞号，把**我方**改成下一个空闲更大号；确认 `migrations/migrations.go` 仍登记我们的迁移。

### 5.4 操作步骤

> 仓库根执行，按顺序，**不要跳过备份**。

#### 第 1 步 · 预检 + 预览
```bash
git status --porcelain                          # 必须为空，否则停下问清楚
git fetch upstream
git log --oneline --graph upstream/main...HEAD | head -40
git log --oneline HEAD..upstream/main           # 上游新增的提交
git diff --stat HEAD..upstream/main | tail -40  # 上游动过的文件
```
把"上游动过的文件"对照 5.3 的桶，预判冲突落点（router/i18n/sidebar/ent/wire/migrations 是常客）。

#### 第 2 步 · 备份分支 + tag（绝不跳过）
```bash
git branch backup/pre-merge-$(git rev-parse --short HEAD)
git tag    pre-merge-$(git rev-parse --short HEAD)
```

#### 第 3 步 · 开始合并
```bash
git merge --no-ff upstream/main
```
即使无冲突自动合上，**也要跑完第 6 步验证门** —— 干净文本合并照样可能丢功能或撞迁移号。

#### 第 4 步 · 按桶分诊冲突
```bash
git diff --name-only --diff-filter=U     # 冲突文件
```
逐个对照 5.3：OURS → `git checkout --ours -- <file> && git add`；DELETED → `git rm <file>`；UNION → **打开手动合，两边新增都保留**，按 5.5 标记核对后再 `git add`，**绝不**整体 `--ours/--theirs`；GENERATED → 不碰冲突标记，取一边得到可解析文件，第 5 步重生成。

#### 第 5 步 · 重生成 + 迁移
```bash
cd backend && go generate ./ent      # 重生成 Ent（含 admin_billing 实体）
cd backend && go generate ./...      # 重生成 Wire 等
```
迁移撞号按 5.3 MIGRATIONS 处理。

#### 第 6 步 · 验证门（提交前必须全绿）
```bash
# 前端（pnpm，不要用 npm）
pnpm --dir frontend install
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
pnpm --dir frontend run test:run
pnpm --dir frontend run build
# 后端
cd backend && go build ./...
cd backend && go test -tags=unit ./...
cd backend && go test -tags=integration ./...
```
再跑 5.5 的存活校验。（Windows 无 make 见坑 8；改了 package.json 要同步 `pnpm-lock.yaml` 见坑 1）

#### 第 7 步 · 提交 & 回滚
- 全绿才提交，沿用默认 merge 信息或写清调和内容；出自定义版本则 bump `VERSION` 并保持发布/部署指向 fork；确认前保留备份分支/tag，未经同意不 push。
- 回滚：`git merge --abort`（中途）/ `git reset --hard backup/pre-merge-<sha>`（已合出坏提交）。

### 5.5 功能存活校验命令

**Presence —— 我的功能还在（每条都必须有结果）**
```bash
grep -nE "AdminBilling|/admin/billing|ImageChat|/image" frontend/src/router/index.ts
# i18n 键是嵌套的（非点号路径），且上游有自己无关的 imageGeneration 键，
# 故匹配我们 image-chat + admin-billing 命名空间里独有的字符串：
grep -nE "imageNofM|retryThisImage|noImageData|failedToLoadStats" frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
grep -n  "imageGeneration:" frontend/src/utils/featureFlags.ts
grep -nE "/image|/admin/billing" frontend/src/components/layout/AppSidebar.vue
ls frontend/src/views/admin/BillingView.vue frontend/src/views/user/ImageChatView.vue
ls backend/internal/service/admin_billing_service.go backend/ent/schema/admin_billing_record.go
ls backend/migrations/*admin_billing*.sql
```

**Absence —— 合规保持移除（每条都必须查无）**
```bash
ls frontend/src/stores/adminCompliance.ts frontend/src/api/admin/compliance.ts 2>/dev/null
ls backend/internal/service/admin_compliance.go backend/internal/handler/admin/compliance_handler.go 2>/dev/null
grep -rn "adminCompliance\|AdminComplianceDialog" frontend/src/stores/index.ts frontend/src/api/admin/index.ts
```
任一 presence 为空、或任一 absence 命中文件 → 合并丢了/复活了功能，**提交前必须修掉**。

### 5.6 需要停下问人的硬停止项
- 预检时工作区不干净。
- 上游重新引入/重构合规模块（默认保持移除，先确认）。
- 某冲突只能靠丢掉一个自定义功能才能解，或上游以不兼容方式重写了我们完全独有的文件。
- 迁移撞号无法明确改号。
- 验证门无论如何绿不了 —— 贴失败输出，**绝不带红提交**。

## 六、常用命令速查

### 数据库操作

```bash
# 连接数据库
psql -U sub2api -h 127.0.0.1 -d sub2api

# 查看所有用户
psql -U postgres -h 127.0.0.1 -c "\du"

# 查看所有数据库
psql -U postgres -h 127.0.0.1 -c "\l"

# 执行 SQL 文件
psql -U sub2api -h 127.0.0.1 -d sub2api -f migration.sql
```

### Git 操作

```bash
# 同步上游（⚠️ 保功能的完整流程见「五、合并上游官方代码」，不要直接无脑 merge）
git fetch upstream
git checkout main
git merge upstream/main
git push origin main

# 创建功能分支
git checkout -b feature/xxx

# Rebase 到最新 main
git fetch upstream
git rebase upstream/main
```

### 前端操作

```bash
# 安装依赖（必须用 pnpm）
cd frontend
pnpm install

# 开发服务器
pnpm dev

# 构建
pnpm build
```

### 后端操作

```bash
# 运行服务器
cd backend
go run ./cmd/server/

# 生成 Ent 代码
go generate ./ent

# 运行测试
go test -tags=unit ./...
go test -tags=integration ./...

# Lint 检查
golangci-lint run ./...
```

## 七、项目结构速览

```
sub2api-bmai/
├── backend/
│   ├── cmd/server/          # 主程序入口
│   ├── ent/                 # Ent ORM 生成代码
│   │   └── schema/          # 数据库 Schema 定义
│   ├── internal/
│   │   ├── handler/         # HTTP 处理器
│   │   ├── service/         # 业务逻辑
│   │   ├── repository/      # 数据访问层
│   │   └── server/          # 服务器配置
│   ├── migrations/          # 数据库迁移脚本
│   └── config.yaml          # 配置文件
├── frontend/
│   ├── src/
│   │   ├── api/             # API 调用
│   │   ├── components/      # Vue 组件
│   │   ├── views/           # 页面视图
│   │   ├── types/           # TypeScript 类型
│   │   └── i18n/            # 国际化
│   ├── package.json         # 依赖配置
│   └── pnpm-lock.yaml       # pnpm 锁文件（必须提交）
└── .claude/
    └── CLAUDE.md            # 本文档
```

## 八、参考资源

- [上游仓库](https://github.com/Wei-Shaw/sub2api)
- [Ent 文档](https://entgo.io/docs/getting-started)
- [Vue3 文档](https://vuejs.org/)
- [pnpm 文档](https://pnpm.io/)
