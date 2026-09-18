# Changelog

## Desktop 3.0.9 / 桌面版 3.0.9

- Windows: restore the standard NSIS / MUI2 wizard and replace PowerShell process detection with a native Go helper. Continue immediately when no target is running and files are replaceable; allow up to 5 seconds for graceful exit, then terminate only confirmed target remnants.
- Windows：恢复标准 NSIS / MUI2 向导，用原生 Go 检查替代 PowerShell 进程检测。无目标进程且文件可替换时立即继续；正常退出最多等待 5 秒，再仅结束已确认目标残留。
- The new shutdown channel saves drafts, attachments, and sessions before exit. Distinguish external file locks, write permissions, and detection failures; keep skipped files prohibited. Forced termination of older unresponsive versions cannot guarantee saving unsaved content.
- 新版退出通道在退出前保存草稿、附件与会话；区分外部文件占用、写入权限和检测失败，继续禁止跳过文件。强制结束无响应旧版本不能保证保存尚未落盘的内容。
- [Release notes / 发布说明](docs/releases/desktop-v3.0.9.md)
- [Verification / 验收](docs/audits/desktop-v3.0.9-verification.md)

## Desktop 3.0.8 / 桌面版 3.0.8

- Modern: center the turn rail and Composer, keep controls on one row with effort before model, and give progress text its own space. Refine neutral buttons and keyboard menus; fix narrow approval sizing and restored-history navigation.
- Modern：修复输入区过宽、控件拥挤、权限入口隐藏和历史定位偏移，统一轻量选择器与运行按钮；Classic 保持不变。
- [Release notes / 发布说明](docs/releases/desktop-v3.0.8.md)
- [Verification / 验收](docs/audits/desktop-v3.0.8-verification.md)

## Desktop 3.0.7 / 桌面版 3.0.7

- Windows: detect 64-bit background processes, close target runtimes before replacement,
  fail closed on unknown process paths or locked files, and disable skipped files.
- Windows：修复后台进程检测与自动关闭，无法确认安全写入时阻止安装，禁止跳过文件。
- [Release notes / 发布说明](docs/releases/desktop-v3.0.7.md)
- [Verification / 验收](docs/audits/desktop-v3.0.7-verification.md)

All notable changes to the Go line (O.R.C.A 1.0+) are recorded here. The legacy
`0.x` TypeScript history lives on the [`v1`](https://github.com/nanbo0ne/O.R.C.A-for-Windows/tree/v1)
branch.

## Desktop 3.0.6 / 桌面版 3.0.6 (Unreleased / 未发布)

### 简体中文

- 修正 Modern 回合导航留白与紧凑导航条，不重设计布局，Classic 保持不变。
- 合并连续工具摘要，保留真实阶段文本及顺序，不替换为通用状态或吞掉中间回复。
- 修正自定义供应商显示 OpenAI-compatible 却保存字母排序首位 Anthropic 类型的问题；后端规范化 Agent 端点，对协议不匹配给出明确的配置修正提示。
- Windows 升级验收基线更新为已发布 3.0.5；3.0.6 验收待补充，不重复 3.0.5 已完成的官方模型迁移。托管本地 AI 与 Computer Use 继续禁用。

### English

- Correct the Modern navigation gutter and compact turn rail, without a layout redesign or changes to Classic.
- Merge consecutive tool summaries while retaining actual stage text and order, without generic replacements or lost intermediate replies.
- Correct custom-provider selection that displayed OpenAI-compatible but saved the alphabetically first Anthropic registration; normalize backend Agent endpoints and provide actionable protocol-mismatch errors.
- Advance Windows upgrade acceptance to the published 3.0.5 baseline. 3.0.6 acceptance remains pending; completed 3.0.5 official-model migration is not repeated. Managed local AI and Computer Use remain disabled.

[Release notes / 发布说明](docs/releases/desktop-v3.0.6.md) · [Pending verification / 待补充验收](docs/audits/desktop-v3.0.6-verification.md).

## [1.0.0] — 2026-06-03

First stable release — a **ground-up rewrite in Go**. Not an upgrade of the `0.x`
TypeScript line; a new codebase that becomes the default (`main-v2`).

### Highlights

- **Go kernel**: a single static binary (CGO-free), cross-compiled for
  darwin/linux/windows on amd64 + arm64. Distributed via npm (the package wraps
  the native binary), Homebrew (`nanbo0ne/O.R.C.A-for-Windows` tap), and release archives;
  no Node runtime needed to run it.
- **Agent core**: the loop, built-in tools (read/write/edit/multi_edit/glob/grep/
  ls/bash/web_fetch/todo_write), permission gate, sandboxed bash, and the
  DeepSeek prefix-cache–oriented design.
- **Subagents**: `task` plus explore/research/review/security_review skill agents.
- **Skills & hooks**: Claude-Code-style skills (`internal/skill`) and hooks
  (`internal/hook`), symlink-aware and slash-integrated.
- **MCP client**: connect external servers over stdio / Streamable HTTP; reads
  `[[plugins]]` and a Claude-Code `.mcp.json`.
- **Code intelligence via CodeGraph**: a tree-sitter symbol/call graph
  (`codegraph_*` tools) replaces embedding semantic search — no embedding service
  or API cost. Fetched into a local cache on first use (or `deepseek-orca codegraph
  install`) and indexed in the background, so installs and startup stay fast.
- **Plan mode** with evidence-backed step sign-off (`complete_step`).
- **Memory**: `DEEPSEEK_ORCA.md` hierarchy + auto-memory, folded into the cache-stable
  prefix.
- **ACP** (`deepseek-orca acp`) and an HTTP/SSE server frontend; desktop app (Wails).

### Fixed

- **File encoding support restored** — GBK/GB18030 (and other non-UTF-8) files
  can now be read, edited, and grepped correctly. The v2 rewrite had dropped
  v1's encoding detection; files in CJK Windows charsets were silently misread
  or rejected as binary. The read/edit/write round-trip now preserves the
  original file encoding. (#2637)

### Notes

- Versions: the legacy TypeScript line stays in `0.x`; the Go line starts at
  `1.0.0`. See [docs/MIGRATING.md](docs/MIGRATING.md).
- Release archives ship a bare binary; CodeGraph is fetched on first use. Windows
  support for the fetched runtime is unverified — install `codegraph` on PATH if
  the auto-fetch doesn't resolve there.

[1.0.0]: https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/v1.0.0
