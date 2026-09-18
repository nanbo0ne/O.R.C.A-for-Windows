# O.R.C.A. Desktop 3.0.10

## 简体中文

- 运行时鼠标主按钮始终用于停止，即使输入了下一轮草稿也不会被发送按钮替换；Enter 仍可提交下一轮，Ctrl/Cmd+Enter 可引导当前回合。
- 取消待发送内容时只向空输入框恢复原文，不覆盖新草稿；发送请求稍后成功也不会清空已取消的草稿、附件和引用。停止请求失败或本地提示不会误报为空闲，等待较久时显示可重试状态。
- 修复思考强度的保存与请求生效，保存期间阻止发送，保存中或运行中禁用强度切换并提示状态；保存失败明确显示。auto 提示区分实际默认值与省略参数后采用模型默认值，不将不同供应商的强度语义混为一谈。
- 对明确声明支持的较高强度（如 `xhigh` / `max`），按接入能力透传，避免错误降为 `high`；未配置明确默认值时不擅自选取列表第一项。保留未显式配置能力的旧接入回退行为。
- 修复当前回合取消与流式请求结束行为；按回合身份处理取消与终态，避免旧请求误取消新回合。生命周期切换清理独立条目状态，迟到的旧回合完成事件保留原始身份且不清空新回合状态。
- 运行中的标签每 500 毫秒核对后端状态，发送请求完成前不提前判定空闲；完成通知丢失时继续恢复终态，启动前失败保留原始错误。取消关闭本地输出通道，不代表远端计算或计费必然终止。
- 沿用 3.0.9 已恢复的标准 Windows NSIS / MUI2 安装向导、原生进程检测及 Modern / Classic 布局。
- 保留现有配置、草稿、会话、自定义供应商及迁移后的主动模型选择；不重复已完成的 3.0.5 官方模型迁移。托管本地 AI 与电脑操控继续禁用。
- Windows 未作 Authenticode 发布者签名，macOS 未公证；Minisign 验证更新完整性，不等于操作系统发布者认证。

## English

- Keep the mouse action on Stop throughout a running turn, even with a next-turn draft. Enter still queues the next request; Ctrl/Cmd+Enter guides the current turn.
- Restore cancelled submission text only into an empty input, preserving a newer draft. A late successful Submit cannot clear the cancelled draft, attachments, or references. Stop failures and local notices retain running state; a longer wait exposes a retry action.
- Fix effort persistence and request behavior, block sends while saving, disable effort changes during saving/running, and show save failures. Auto hints distinguish a known effective default from omitting the parameter for the model default; provider effort semantics remain distinct.
- For explicitly supported higher levels such as `xhigh` / `max`, preserve the endpoint-declared value rather than incorrectly clamping it to `high`. Do not invent a default from the first list entry when none is configured. Retain legacy fallbacks for endpoints without explicit capabilities.
- Fix current-turn cancellation and streaming-request termination, using turn identity to keep stale cancellation from targeting a successor. Lifecycle transitions reset per-turn item state; late old completions retain their identity without clearing the successor.
- Reconcile running tabs with backend status every 500ms, without reporting idle before Submit resolves. Recover lost completion notifications and preserve original errors from failures before agent startup. Cancellation closes the local output channel; it does not guarantee remote compute or billing termination.
- Retain the standard Windows NSIS / MUI2 wizard, native process checks restored in 3.0.9, and Modern / Classic layouts.
- Preserve configuration, drafts, sessions, custom providers, and deliberate post-migration model selections. Do not repeat completed 3.0.5 official-model migration. Managed local AI and Computer Use remain disabled.
- Windows packages lack Authenticode publisher signing; macOS packages are not notarized. Minisign verifies update integrity, not operating-system publisher identity.

验证范围、发布检查结果及按用户要求暂缓的原生窗口交互验证见详细验收记录。See the detailed verification record for coverage, release checks, and native-window interaction testing deferred at the user's request.

[Release / 发布入口](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.10) · [Detailed verification / 详细验收记录](../audits/desktop-v3.0.10-verification.md) · [Build checklist / 构建清单](../build/desktop-v3.0.10.md)
