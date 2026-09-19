# O.R.C.A. Desktop 3.0.11

## 简体中文

3.0.11 是面向稳定性的补丁版本，保留现有 Modern / Classic 界面、会话、配置、自定义供应商、DeepSeek 模型和升级方式。

- 修复 `/compact` 对本地或第三方 OpenAI-compatible 模型的兼容处理。压缩请求继续使用当前 Provider、端点和模型；真实 `model_not_found` 不会被误判为思考强度错误，也不会偷偷切换模型或供应商。
- 对明确的推理参数不兼容、请求形状不兼容和临时网络错误提供一次受控的压缩专用回退，不修改用户的模型或思考强度偏好。压缩失败时保留完整历史，自动压缩进入短暂退避，避免重复失败。
- 修复压缩成功前提前归档的问题，并增加模型切换、Controller 代次和余额结果保护，旧请求不能覆盖新模型状态。
- 处理中状态回到底部 Composer 控制行，不再使用悬浮状态气泡。`Enter` 发送，`Shift+Enter` 换行。
- 修复图片粘贴可能产生重复附件的问题；Composer 空白区域点击可直接聚焦输入框，按钮、菜单和选择器保持独立操作。
- 合并重复的会话首屏加载请求，使新对话更快显示可交互输入区；保留辅助信息异步加载和旧结果隔离。
- 托管本地 AI 与 Computer Use 继续暂时禁用；普通附件图片识别不受影响。

本版本未把压缩失败伪装成已修复的上游模型问题。若服务端明确返回 `404 model_not_found`，请刷新模型列表、检查端点中的模型 API 名称或手动切换到该端点实际开放的模型。

Windows 安装器仍使用标准 NSIS / MUI2 安装方式。Windows 未作 Authenticode 发布者签名，macOS 未公证；Minisign 仅验证更新完整性，不等于操作系统发布者认证。

## English

3.0.11 is a stability patch. It preserves the existing Modern / Classic UI, sessions, configuration, custom providers, DeepSeek models, and update mechanism.

- Fix compatibility handling for `/compact` with local and third-party OpenAI-compatible models. Compaction keeps the active provider, endpoint, and model; a real `model_not_found` response is not mislabeled as an effort error and never triggers a silent model or provider switch.
- Add one bounded compaction-only fallback for explicit reasoning-parameter rejection, request-shape rejection, and transient provider failures. User model and reasoning preferences are not changed. Failed compaction keeps the complete history, and automatic compaction backs off briefly instead of repeating the same failure.
- Avoid archiving before a valid summary exists, and guard model-switch Controller generations and balance responses so stale work cannot overwrite the new model state.
- Move the processing state into the Composer control row instead of a floating status bubble. `Enter` sends and `Shift+Enter` inserts a newline.
- Prevent duplicate image attachments from clipboard paste and focus the textarea when the blank Composer surface is clicked, while leaving buttons, menus, and selectors independent.
- Coalesce duplicate session first-paint loads so a new conversation becomes interactive sooner; auxiliary metadata remains asynchronous and stale results are isolated.
- Managed local AI and Computer Use remain temporarily disabled; ordinary attached-image analysis is unaffected.

This release does not pretend to fix an upstream model availability problem. When the provider returns an explicit `404 model_not_found`, refresh the model list, check the model API name exposed by the endpoint, or select a model that the endpoint actually serves.

Windows continues to use the standard NSIS / MUI2 installer. Windows packages are not Authenticode publisher-signed and macOS packages are not notarized; Minisign verifies update integrity, not operating-system publisher identity.

[Release / 发布入口](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.11) · [Detailed verification / 详细验收记录](../audits/desktop-v3.0.11-verification.md) · [Build checklist / 构建清单](../build/desktop-v3.0.11.md)
