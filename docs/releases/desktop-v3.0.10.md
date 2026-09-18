# O.R.C.A. Desktop 3.0.10

## 简体中文

- 修复已实现，发布验收尚未完成。五项真实供应商请求通过，原生 Wails 构建通过；全量回归、CI、多平台发行包、升级与发布检查仍待完成。原生窗口交互未测试。
- 修复思考强度的保存与请求生效，保存期间阻止发送，保存中或运行中禁用强度切换并提示状态；保存失败明确显示。auto 提示区分实际默认值与省略参数后采用模型默认值，不将不同供应商的强度语义混为一谈。
- 对明确声明支持的较高强度（如 `xhigh` / `max`），按接入能力透传，避免错误降为 `high`；未配置明确默认值时不擅自选取列表第一项。保留未显式配置能力的旧接入回退行为。
- 修复当前回合取消与流式请求结束行为；按回合身份处理取消与终态，避免旧请求误取消新回合。生命周期切换清理独立条目状态，迟到的旧回合完成事件保留原始身份且不清空新回合状态。
- 真实供应商验证：Token Lens `xhigh` 原值透传、`auto` 省略力度参数；官方 DeepSeek `low`、`auto → high` 及首个 reasoning 片段后取消全部通过。取消证明本地输出通道关闭，不代表远端计算或计费终止。
- 沿用 3.0.9 已恢复的标准 Windows NSIS / MUI2 安装向导及原生进程检测，不修改安装器实现，不改版 Modern / Classic。升级验收基线固定为已发布 3.0.9。
- 保留现有配置、草稿、会话、自定义供应商及迁移后的主动模型选择；不重复已完成的 3.0.5 官方模型迁移。托管本地 AI 与电脑操控继续禁用。
- Windows 未作 Authenticode 发布者签名，macOS 未公证；Minisign 验证更新完整性，不等于操作系统发布者认证。

## English

- Fixes are implemented; release acceptance is incomplete. Five live provider cases and the native Wails build passed. Full regression, CI, platform release packages, upgrade and publication checks remain pending. Native-window interaction was not tested.
- Fix effort persistence and request behavior, block sends while saving, disable effort changes during saving/running, and show save failures. Auto hints distinguish a known effective default from omitting the parameter for the model default; provider effort semantics remain distinct.
- For explicitly supported higher levels such as `xhigh` / `max`, preserve the endpoint-declared value rather than incorrectly clamping it to `high`. Do not invent a default from the first list entry when none is configured. Retain legacy fallbacks for endpoints without explicit capabilities.
- Fix current-turn cancellation and streaming-request termination, using turn identity to keep stale cancellation from targeting a successor. Lifecycle transitions reset per-turn item state; late old completions retain their identity without clearing the successor.
- Live verification passed Token Lens `xhigh` passthrough and `auto` omission, plus official DeepSeek `low`, `auto → high`, and cancellation after the first reasoning delta. Cancellation proves local output-channel closure, not remote compute or billing termination.
- Retain the standard Windows NSIS / MUI2 wizard and native process checks restored in 3.0.9. Installer implementation and Modern / Classic layouts are unchanged. Pin upgrade acceptance to the published 3.0.9 installer.
- Preserve configuration, drafts, sessions, custom providers, and deliberate post-migration model selections. Do not repeat completed 3.0.5 official-model migration. Managed local AI and Computer Use remain disabled.
- Windows packages lack Authenticode publisher signing; macOS packages are not notarized. Minisign verifies update integrity, not operating-system publisher identity.

[Planned Release / 待发布入口](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.10) · [Verification / 验收记录](../audits/desktop-v3.0.10-verification.md) · [Build checklist / 构建清单](../build/desktop-v3.0.10.md)
