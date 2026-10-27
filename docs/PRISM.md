# 内置 Prism 模式

Prism 模式使用 sub2api 管理的 Python/Chromium 子进程访问 Prism 网页。无需单独部署或配置 prism-bridge 地址和密钥。通过管理端生成登录链接即可取得 **Prism 登录会话**，不需要手动提取 Cookie；现有 Codex OAuth / API Key 不能直接代替它。“不降智”是上游项目的描述，不是质量保证。

## 部署

默认 Alpine 镜像不含浏览器。仓库根目录构建带浏览器的镜像：

```sh
docker build --target prism -f deploy/Dockerfile -t sub2api:prism-local .
```

使用现有 Compose 配置可在 `deploy/` 运行：

```sh
docker compose -f docker-compose.yml -f docker-compose.prism.yml up -d --build
```

非 Docker 部署，在后端运行用户环境中安装 Python 3.10+、`playwright==1.58.0`，然后执行 `python -m playwright install --with-deps chromium`。Linux 还需安装 `xvfb`（如 `apt-get install xvfb`），供远程登录使用；不需要物理桌面。Windows 登录使用 headless Chromium。二进制内嵌桥接脚本，不依赖运行目录中的源码。

| 服务端环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| `SUB2API_PRISM_PYTHON` | Linux `python3` / Windows `python` | Python 可执行文件路径 |
| `SUB2API_PRISM_DATA_DIR` | `data/prism` | 私有浏览器和会话数据目录，需可写并持久化 |
| `SUB2API_PRISM_MAX_WORKERS` | `4` | 同时驻留的账号浏览器数量上限 |
| `SUB2API_PRISM_BROWSER` | Playwright Chromium | 可选系统 Chromium/Chrome 可执行文件路径 |

浏览器使用账号配置的代理，同时访问 `prism.openai.com` 和 `sentinel.openai.com`。容器应配置 `init: true`；建议至少 1 GB 共享内存，并根据账号数量预留浏览器内存。

## 首次登录及开关

1. 打开 **账号 → 编辑 OpenAI 账号 → Prism 模式**，开启开关，点击 **生成 Prism 登录链接**。新建时选择 **OpenAI → API Key**，也可直接完成 Prism 登录，无需填写 OpenAI API Key。
2. 点击生成的链接，在新标签页中的远程浏览器里完成 Prism 登录。点击画面中的输入框可直接键入/粘贴，也可使用下方文字输入框发送邮箱、密码或验证码。链接只供当前 sub2api 管理员使用，10 分钟后过期；请保持原账号编辑窗口打开。
3. 登录成功后返回账号编辑窗口，点击 **保存**。Cookie 由后端自动绑定到该账号，不会返回前端。保存前关闭账号窗口会取消会话；保存失败可以在有效期内重试。已有 OAuth、Setup Token、API Key 账号均可开启，Spark 影子账号不支持。
4. 设置可用模型，例如 `gpt-6.1-sol`、`gpt-5.6-sol`、`gpt-5.6-terra`、`gpt-6-luna`、`auto`；`gpt-6-sol` 是上游别名。原来的其他模型名可通过账号模型映射指向 Prism 模型。
5. 使用账号的普通连接测试验证登录和浏览器环境。首次调用需要启动浏览器，可能等待几十秒。登录失效时重新生成链接并保存即可。已有 `auth.json` 仍可在“手动导入（可选）”中导入。

登录浏览器运行在服务器上，并使用生成链接时选择的账号代理。Linux 使用每会话独立的 Xvfb 显示；只通过已鉴权的 sub2api API 传输画面和输入，不开放 VNC/CDP 端口。每次登录使用新 profile，完成、取消或超时后关闭浏览器并清理 profile；同时最多 4 个登录会话。会话状态保存在当前进程内，重启会失效，多副本部署需要会话保持。

此流程不是 Prism 官方 OAuth callback。远程画面不支持本机 Passkey/安全密钥或本机 Google 已登录状态，优先使用邮箱、密码、验证码。第三方身份提供商、Cloudflare 或 OpenAI 的自动化限制仍可能阻止登录；不会绕过这些限制。失败时可重试登录或使用可选手动导入。

关闭开关恢复原账号转发通道；如果是只提供 Prism 登录的新账号，需先补充普通 API Key 才能使用关闭后的通道。开关和凭证只能单账号编辑，避免把一个人的浏览器会话批量绑定到其他账号。

## 行为与范围

- 接入 `/v1/responses`、`/v1/chat/completions`、`/v1/messages`；支持流式及非流式、客户端执行工具、图片输入和全历史重放续接。
- 每账号一个独立进程和浏览器 profile；不同 sub2api API Key 的续接身份隔离。每账号仅允许一轮同时执行，建议并发设为 1。达到浏览器数量限制时，优先回收空闲进程；空闲 10 分钟自动退出。
- 请求不转发 Codex OAuth token 给 Prism。凭证写入敏感 `credentials.prism_cookie`，开关为 `extra.prism_enabled`；子进程通过 stdin 获取凭证和临时桥接密钥，HTTP 仅监听随机 loopback 端口。日志不输出浏览器原始诊断正文。
- 保留完整客户端 instructions，关闭上游的有损历史摘录及模型静默回退。超过 Prism 发送限制时明确报错。服务器本地路径不会被当作图片读取，图片需提供 `data:image/...;base64,...`；HTTP 图片 URL 不在此版本支持范围内。
- Prism 完成生成后才返回正文，流中先有心跳，并非逐 token 上游输出。工具实际在客户端执行，不在 sub2api 服务器执行。
- 不支持 WebSocket、`/responses/compact`、`previous_response_id`、后台响应、原生托管搜索和图片生成。使用 HTTP SSE、完整历史与客户端压缩。
- 已提交请求的超时、断流和未知失败不自动重试，不自动切回 Codex。用量沿用桥接返回值；当前固定版本的 prism-bridge 不返回 token 用量，因此日志中的 token 数为 0，无法据此做准确的按 token 计费。

## 来源

内嵌 [yyyllllming/prism-bridge](https://github.com/yyyllllming/prism-bridge)，固定提交 `a12aaedbbc6c23001c3a3403e99cc370c4f2c44a`，MIT 授权。原始 `bridge.py` 和许可证位于 `backend/internal/pkg/prismbridge/scripts/`，sub2api 的进程生命周期及账号隔离适配位于 `managed.py` 和 Go 管理器；远程登录适配位于 `login.py`。Prism 网页接口变化可能需要更新适配。
