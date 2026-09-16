[English release notes](#english-candidate-notes) | **简体中文**

# O.R.C.A. Desktop 3.0.5

## 3.0.5 发布准备

O.R.C.A. 是覆盖助手、编程、ORCA Agent、多供应商、项目与会话、图片、办公产物、工程工具、子代理、MCP、Skill、记忆和自动化的桌面工作区。完整产品说明见[中文 README](../README.md)与[English README](../README.en.md)。本页是当前开发指南；历史发布说明和审计文档保持原样。

**状态：尚未发布。** [3.0.5 双语说明](../docs/releases/desktop-v3.0.5.md)描述目标行为，[构建与验收清单](../docs/build/desktop-v3.0.5.md)区分实测和待办。[目标下载页](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.5)待正式发布后可用；已发布安装基线为 [3.0.4](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.4)。旧版本通过记录不能替代 3.0.5 验收。

- 官方规范引用为 `deepseek/deepseek-flash`，API 模型名 `deepseek-flash`，对应 DeepSeek V4.1 Flash：原生视觉、1M token 上下文、最大 384K token 输出、工具调用与 JSON Output。384K 是能力上限，不是每次请求的默认输出预算。
- `deepseek-v4-flash`、`deepseek-v4-flash-vision-exp` 仅保留为兼容别名，由官方路由至 V4.1 Flash 并按 Flash 计费。`deepseek/deepseek-v4-pro` 仍可选择，保持文本模型，不支持图片。
- 已有官方 DeepSeek 默认值、角色和保存的会话模型选择一次性升级至 Flash，包括原先的官方 Pro；迁移后可重新选择 Pro，后续启动不强制改回。自定义供应商、代理端点、独立凭据与非官方选择保留，历史消息、会话 ID、附件和已存金额不重写。
- 首次配置可验证 DeepSeek Key 或跳过；未独立配置的普通子代理继承主模型。视觉角色先取明确配置，再取确认支持图片的当前模型，最后使用官方 Flash；图片读取和发送权限仍需通过，关闭视觉保持有效。
- 思考强度为 `low` / `high` / `max`，`auto` 使用 `high`。思考模式开关与 effort 独立；简略/详细仅控制 reasoning 显示。
- 真实文本增量、阶段回复与工具按序展示；完成摘要显示耗时、本轮 token 与可确认费用，最终答案独立显示。工作区图片经权限、路径、格式与限额检查后形成快照再委派识图。
- 托管本地 AI 与 Computer Use 在所有平台继续暂时禁用；停止/清理接口、代码、配置和模型文件保留，外部兼容本地服务仍可配置。Modern 与 Classic 保留，本次不开展 DPI 工作。
- 下载显示来源、实测速度和 ETA；取消后可切换同一签名包的来源并续传。下载速度取决于网络与服务器，不保证换源更快；模型响应时间也不作未经实测的承诺。新包不生成旧品牌重复资产，安装兼容保留。

Windows 版本注入示例（在 `desktop` 下）：`wails build -platform windows/amd64 -ldflags "-X main.version=v3.0.5 -X main.channel=stable" -nsis -webview2 embed`。该命令不是完整发布包或安装验收通过的证明。

DeepSeek 官方价格于 2026-09-17 核对，单位 CNY/百万 token，别名与 Flash 同价：

| 模型与时段 | 缓存命中输入 | 未命中输入 | 输出 |
| --- | ---: | ---: | ---: |
| V4.1 Flash 闲时 | ¥0.02 | ¥1 | ¥4 |
| V4.1 Flash 峰时 | ¥0.04 | ¥2 | ¥8 |
| Pro 闲时 | ¥0.15 | ¥4.5 | ¥13.5 |
| Pro 峰时 | ¥0.30 | ¥9 | ¥27 |

峰时固定为 UTC+8 周一至周五 09:00-12:00、14:00-18:00，其余含周末为闲时。每次请求开始时冻结计价依据；历史金额不重算、不改币种，USD/CNY 混合会话不显示为一个权威总额。来源：[模型与价格](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)、[更新日志](https://api-docs.deepseek.com/zh-cn/updates)、[思考模式](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/)。

<a id="english-candidate-notes"></a>

## 3.0.5 Release Preparation

O.R.C.A. remains a full desktop workspace for Assistant, Coding, ORCA Agent, providers, projects/sessions, images, office artifacts, engineering tools, subagents, MCP, Skills, memory, and automation. See the [English README](../README.en.md) and [Chinese README](../README.md). This is the current development guide; historical release notes and audits are retained.

**Status: not published.** The [bilingual notes](../docs/releases/desktop-v3.0.5.md) describe intended behavior; the [build checklist](../docs/build/desktop-v3.0.5.md) separates local results from pending acceptance. The [3.0.5 target download page](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.5) is available only after publication. The published installer baseline is [3.0.4](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.4); its tests do not establish 3.0.5 acceptance.

- Official canonical reference `deepseek/deepseek-flash` and API model `deepseek-flash` identify DeepSeek V4.1 Flash: native vision, 1M context, up to 384K output tokens, tools, and JSON Output. The maximum output is not the default request budget.
- `deepseek-v4-flash` and `deepseek-v4-flash-vision-exp` are compatibility aliases routed to V4.1 Flash at Flash rates. `deepseek/deepseek-v4-pro` remains selectable and text-only, without image support.
- Official defaults, roles, and saved session model selections receive a one-time Flash upgrade, including prior official Pro selections. Pro can be selected again afterward and survives later startups. Custom providers, proxy endpoints, separate credentials, non-official choices, historical messages, session IDs, attachments, and stored amounts are preserved.
- First run validates a DeepSeek key or skips setup. Ordinary subagents inherit the main model unless configured separately. Vision uses the explicit role, then a confirmed capable current model, then official Flash. Image read/send permissions and explicit vision disablement still apply.
- Effort supports `low` / `high` / `max`; `auto` resolves to `high`. Thinking and effort are separate; Compact/Detailed only affects reasoning visibility.
- Text deltas, stage replies, and tools remain ordered. Completed summaries show elapsed time, tokens, and known cost, with final answers separate. Generated images require permission and validated snapshots before delegation.
- Managed local AI and Computer Use stay disabled on all platforms. Stop/cleanup, code, configuration, and model files remain; external compatible local services can be configured. Modern and Classic remain. No DPI work is included.
- Downloads show source, measured speed, and ETA; cancel before switching sources for the same signed payload. Speed depends on the network and server, and switching does not guarantee an improvement. No unmeasured model-response speed is promised. New builds omit old-brand duplicates while retaining installer compatibility.

Use the Windows version-injection command above. It is not proof of a complete release bundle or passed installer acceptance.

Official CNY per million tokens, checked on 2026-09-17; Flash aliases use the same rates:

| Model and period | Cache-hit input | Cache-miss input | Output |
| --- | ---: | ---: | ---: |
| V4.1 Flash off-peak | ¥0.02 | ¥1 | ¥4 |
| V4.1 Flash peak | ¥0.04 | ¥2 | ¥8 |
| Pro off-peak | ¥0.15 | ¥4.5 | ¥13.5 |
| Pro peak | ¥0.30 | ¥9 | ¥27 |

Peak time is fixed UTC+8 Monday-Friday 09:00-12:00 and 14:00-18:00; all other times, including weekends, are off-peak. Each request freezes pricing at start. Stored amounts are not recalculated or relabeled; mixed USD/CNY sessions do not display one authoritative combined total. Sources: [pricing](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/), [updates](https://api-docs.deepseek.com/zh-cn/updates), [thinking mode](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/).

## 架构与开发

这是 O.R.C.A. Go 内核的 Wails 桌面壳。React + TypeScript 前端通过 Wails typed bindings 直接调用 `desktop/app.go`，Go 侧把 `control.Controller`、Provider、工具、MCP、Skill、会话、产物、本地 AI 和权限事件绑定到 WebView；没有额外的 HTTP hop。

**3.0.5 所有平台继续暂时禁用托管本地 AI 与电脑操控。** 代码、配置和模型文件保留；普通图片与文件附件及外部兼容本地服务仍可用。原生测试与发布状态以本版验收记录为准。

English follows the Chinese section.

## 目录与边界

| 路径 | 用途 |
| --- | --- |
| `desktop/app.go` | Wails 方法、会话标签、配置、附件和事件桥接 |
| `desktop/main.go` | 窗口、平台 WebView、菜单、拖放和嵌入前端 |
| `desktop/updater*.go` | 更新检查、签名校验和平台动作 |
| `desktop/computer_use*.go` | 保留的 Computer Use 会话、观察、授权和动作代码；本版本所有平台禁用 |
| `desktop/local_ai_app.go` | Windows 本地运行时、模型目录和下载状态 |
| `desktop/frontend/src/` | Modern/Classic UI、Composer、会话、设置、工具和右侧工作区 |
| `desktop/build/` | Wails、Linux、Windows 安装和发布所需源文件 |

`desktop/` 是独立 Go module，通过 `replace` 引用父目录内核。它与 CLI 的构建隔离，因为桌面端使用 CGO 和各平台 WebView；前端独立运行时的 mock 只用于布局开发，不证明原生能力。

## 本地开发

先安装 Go、Node.js、npm 和 Wails CLI v2：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
cd desktop
npm --prefix frontend install
wails dev
```

只调前端时：

```powershell
cd desktop\frontend
npm install
npm run dev
```

浏览器 mock 可以展示消息流、Markdown、工具卡片和部分布局，但没有 Wails bindings、原生文件拖放、窗口框架、GPU/WebView、Local AI 或 Computer Use。不要用它作为桌面验收。

## 构建

```powershell
cd desktop\frontend
npm install
npm run build

cd ..
go test .
wails build
```

从仓库根目录执行内核测试：

```powershell
go test ./...
```

Windows 安装器还需要 NSIS。Windows 使用 Edge WebView2 Runtime；macOS 12 或更新版本使用系统 WebKit（[Go 工具链最低要求](https://go.dev/doc/go1.25#darwin)）；Linux 需要 GTK 和 WebKitGTK 开发/运行库。发行版使用 WebKitGTK 4.1 时，按本机 Wails/发行版配置使用 `-tags webkit2_41`：

```sh
wails build -tags webkit2_41
wails dev -tags webkit2_41
```

`frontend/dist` 由构建生成。没有先执行前端构建时，Go embed 可能只有占位目录，窗口会白屏或缺少资源；这不是 Provider 或模型错误。

## 桌面产品行为

- Modern 是默认工作壳；Classic 保留 V2.1.3 蓝白布局和原生窗口框架。样式选择持久化，Windows 壳切换通常在重启后完全生效。
- Assistant、Coding 和固定 Orca 三种工作入口共享内核，但分别使用不同的提示词、工具和记忆边界。
- Provider、主模型、planner、subagent 和自动化角色分别解析完整 `provider/model` 引用。官方规范引用及未配置的视觉候选为 `deepseek/deepseek-flash`；自定义角色保留，旧官方角色按一次性迁移规则升级。原生视觉不自动授予附件或上传权限。Computer Use 控制角色仅保留配置，不运行，包内不含 API key。
- Computer Use 在 Windows、macOS、Linux 均暂时禁用。已有授权、配置、Full access、恢复会话及派发任务都不能重新启用它；代码和配置保留供后续验收恢复。
- O.R.C.A 管理的 `llama.cpp` 运行时和模型下载在 Windows、macOS、Linux 均暂时禁用，停止、取消和清理接口保留，已有文件不删除。外部兼容本地服务可手动配置；应用不接管 LM Studio。
- 图片和附件属于当前回合上下文；结构化 DOCX/XLSX/PPTX/PDF 产物使用 sidecar、重新解析和真实渲染器限制，详情见 [`../docs/ARTIFACT_RUNTIME.md`](../docs/ARTIFACT_RUNTIME.md)。
- 可用示例：手动附图并要求“识别表格并生成 CSV”，或引用项目文件要求审查；无需启动电脑操控。

## 更新与分发

3.0.5 保留现有 stable updater 流程；实际更新须等待签名发布资产可用：

1. 首先检查 [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json)，失败时回退 GitHub 的签名 manifest 和签名 payload。
2. Windows 已安装版本只在用户明确操作后下载；下载完成后提示退出应用，再运行安装器。不得后台自动下载或自动安装。
3. macOS/Linux 展示版本与完整性信息并打开下载页/包；不承诺跨平台原位自更新。
4. 无可用应用内更新路径的旧版用户需从已发布的 Release 页手动安装一次。3.0.5 的[目标发布页](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.5)仅在正式发布后提供可下载资产；升级前备份配置和会话。

签名校验必须先于落盘或替换安装。私钥只存在发布环境，不进入仓库、manifest 示例或文档。当前待验收项见[3.0.5 构建清单](../docs/build/desktop-v3.0.5.md)。[3.0.3 验证报告](../docs/audits/2026-09-08-v3.0.3-validation.md)仅保留为历史记录；单测或旧版验收不能证明本版安装或发布验收完成。

## 平台排查

- Windows 白屏：确认 WebView2 Runtime、显卡驱动和 WebView2 GPU 设置；先用 Classic/Modern 对照，再收集日志。不要把浏览器 mock 的成功当成原生成功。
- Linux 白屏或闪烁：确认 GTK/WebKitGTK 版本和发行版包；必要时按平台文档使用 `WEBKIT_DISABLE_COMPOSITING_MODE=1` 做诊断。不同 GPU/发行版必须单独验证。
- macOS 首次打开：检查 DMG、签名/公证和系统 Gatekeeper 提示。不要把清除隔离属性当成正式签名替代方案。
- 拖放/剪贴板异常：确认 Wails 运行窗口、路径是否位于工作区、附件是否可读；浏览器 mock 没有同等的原生路径语义。
- Computer Use 不可用：3.0.5 所有平台继续暂时禁用，授权、旧配置或更换控制模型均不能开启；普通 Vision 识图和附件处理仍可用。
- 托管本地模型入口不可用：本版按设计禁用，不要尝试用旧配置开启。外部兼容本地服务请求失败时检查端点、凭据与服务自身日志。
- 更新检查失败：分别检查主 manifest、GitHub 回退、系统代理、签名和版本字段。Windows 按“下载、退出、安装”的显式流程处理。

## 发布前检查

- 先构建前端，再运行 `go test .` 和需要的根模块测试；测试结果只能代表运行过的范围。
- 在目标 OS 上验证窗口、WebView、拖放、安装/升级、一次性设置及会话模型迁移、Provider 请求、附件、产物、权限，以及托管本地 AI 的禁用边界；没有实机证据就标为未验证。
- 必须验证全平台不注册电脑操控工具，直接调用、旧配置/授权、会话恢复、自动化和派发均无法绕过禁用，截图与原生输入调用次数为零，并独立运行普通附件视觉回归。本次不开展 DPI 工作；自动测试不等于真机验收。
- 生成并核对 manifest、payload、签名和 SHA-256，并与 Release 页文件逐项对应。
- 保留旧用户目录和会话备份；不要在构建或文档任务中修改服务器、网站、密钥、版本配置或无关目录。

---

# O.R.C.A. Desktop 3.0.5 (English Development Guide)

This is the Wails desktop shell around the O.R.C.A. Go kernel. The React + TypeScript frontend calls `desktop/app.go` through typed Wails bindings. The Go side binds `control.Controller`, providers, tools, MCP, Skills, sessions, artifacts, local AI, and permission events to the WebView without an extra HTTP hop.

**Managed local AI and Computer Use remain temporarily disabled on every platform in 3.0.5.** Code, configuration, and model files remain. Ordinary image/file attachments and external compatible local services remain usable. Native tests and release status depend on this version's acceptance evidence.

## Layout and Boundary

| Path | Purpose |
| --- | --- |
| `desktop/app.go` | Wails methods, session tabs, config, attachments, and event bridge |
| `desktop/main.go` | Window, platform WebView, menu, file drops, and embedded frontend |
| `desktop/updater*.go` | Update checks, signature verification, and platform actions |
| `desktop/computer_use*.go` | Retained Computer Use session, observation, consent, and action code; disabled on all platforms in this version |
| `desktop/local_ai_app.go` | Windows local runtime, model directory, and download state |
| `desktop/frontend/src/` | Modern/Classic UI, Composer, sessions, settings, tools, and workspace dock |
| `desktop/build/` | Wails, Linux, Windows installer, and release source files |

`desktop/` is a nested Go module that uses `replace` to import the kernel from the parent directory. It is separate from the CLI build because the desktop target uses CGO and the native WebView on each OS. The standalone frontend mock is for layout work only and does not prove native behavior.

## Local Development

Install Go, Node.js, npm, and Wails CLI v2:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
cd desktop
npm --prefix frontend install
wails dev
```

For frontend-only work:

```powershell
cd desktop\frontend
npm install
npm run dev
```

The browser mock can show message streaming, Markdown, tool cards, and parts of the layout, but it has no Wails bindings, native file-drop semantics, window frame, GPU/WebView, Local AI, or Computer Use. Do not use it as desktop acceptance.

## Build

```powershell
cd desktop\frontend
npm install
npm run build

cd ..
go test .
wails build
```

Run kernel tests from the repository root:

```powershell
go test ./...
```

The Windows installer also needs NSIS. Windows uses Edge WebView2 Runtime; macOS 12 or later uses system WebKit (the [Go toolchain minimum](https://go.dev/doc/go1.25#darwin)); Linux needs GTK and WebKitGTK development/runtime libraries. On distributions using WebKitGTK 4.1, use the local Wails/distribution setting and, where required, `-tags webkit2_41`:

```sh
wails build -tags webkit2_41
wails dev -tags webkit2_41
```

`frontend/dist` is generated by the build. Without a frontend build first, Go embed may contain only the placeholder directory and the window may be blank or missing resources; that is not a provider or model failure.

## Desktop Product Behavior

- Modern is the default work shell; Classic retains the V2.1.3 blue-and-white layout and native frame. The style choice persists, and the Windows shell normally switches fully after restart.
- Assistant, Coding, and the fixed Orca entry share the kernel but use distinct prompt, tool, and memory boundaries.
- Providers, main model, planner, subagent, and automation roles resolve independent fully qualified `provider/model` references. The official canonical reference and unconfigured vision candidate are `deepseek/deepseek-flash`. Custom roles remain; old official roles follow the one-time migration. Native vision does not automatically grant attachments or upload permission. The Computer Use role is retained as configuration only and does not run; packages contain no API key.
- Computer Use is temporarily disabled on Windows, macOS, and Linux. Existing consent, configuration, Full access, restored sessions, and dispatched tasks cannot re-enable it. Code and configuration remain for restoration after later validation.
- O.R.C.A.-managed `llama.cpp` and model downloads remain temporarily disabled on Windows, macOS, and Linux. Stop, cancel, and cleanup interfaces remain, and existing files are retained. External compatible local services can be configured manually; LM Studio is not controlled.
- Images and attachments are current-turn context. Structured DOCX/XLSX/PPTX/PDF artifacts use sidecars, reparse checks, and real renderer limits; see [`../docs/ARTIFACT_RUNTIME.md`](../docs/ARTIFACT_RUNTIME.md).
- Example: attach an image manually and ask, "Extract its table as CSV," or reference a project file for review. These tasks do not require Computer Use.

## Updates and Distribution

3.0.5 retains the existing stable updater flow; actual updates require published signed assets:

1. Check [`https://orca.aichat.diy/updates/stable/latest.json`](https://orca.aichat.diy/updates/stable/latest.json) first, then fall back to GitHub's signed manifest and signed payload.
2. An installed Windows build downloads only after an explicit user action; after the download it asks the user to exit and then runs the installer. No background download or installation is allowed.
3. macOS/Linux show the version and integrity information and open the download page/package; in-place cross-platform self-update is not promised.
4. Older versions without a working in-app update path need one manual installation from a published Release page. The [3.0.5 target release](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.5) provides downloadable assets only after publication. Back up configuration and sessions before upgrading.

Signature verification must happen before writing or replacing an installation. Private signing keys stay in the release environment and never enter the repository, example manifest, or documentation. The [3.0.5 build checklist](../docs/build/desktop-v3.0.5.md) records pending acceptance. The [3.0.3 validation report](../docs/audits/2026-09-08-v3.0.3-validation.md) is retained as historical evidence only. Unit tests and older acceptance runs do not establish this version's installation or release acceptance.

## Platform Troubleshooting

- Windows blank window: check WebView2 Runtime, GPU drivers, and WebView2 GPU settings. Compare Classic and Modern, then collect logs. Browser-mock success is not native success.
- Linux blank or flickering window: check GTK/WebKitGTK versions and distribution packages. Where needed, use `WEBKIT_DISABLE_COMPOSITING_MODE=1` for diagnosis. Each GPU/distribution needs separate validation.
- macOS first launch: check the DMG, signing/notarization state, and Gatekeeper prompt. Clearing quarantine is not a substitute for formal signing.
- Drop/clipboard issue: confirm a Wails window, workspace path boundaries, and readable attachments. The browser mock does not have the same native path semantics.
- Computer Use unavailable: it remains temporarily disabled on all platforms in 3.0.5. Consent, old configuration, and changing the control model cannot enable it; ordinary Vision and attachment processing remain supported.
- Managed local model entry points are disabled by design; old configuration cannot enable them. For a custom external local service, inspect its endpoint, credentials, and service logs.
- Update check failure: inspect the primary manifest, GitHub fallback, system proxy, signature, and version fields separately. On Windows follow the explicit download, exit, install flow.

## Release Checklist

- Build the frontend first, then run `go test .` and the necessary root-module tests; a result covers only the executed scope.
- On target OSes, validate window/WebView, drops, install/upgrade, one-time configuration/session model migration, provider requests, attachments, artifacts, permissions, and managed local AI disablement. Without native evidence, mark the item unvalidated.
- Verify that no platform registers computer-control tools; direct calls, old settings/consent, restored sessions, automation, and dispatch must not bypass disablement. Screen-capture and native-input calls must stay at zero. Run ordinary image-attachment regression checks separately. No DPI work is included; automated tests do not establish native acceptance.
- Generate and compare the manifest, payload, signatures, and SHA-256 values against the files on the Release page.
- Preserve old user roots and session backups. Do not modify servers, websites, keys, version configuration, or unrelated directories during a documentation/build task.
