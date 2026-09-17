# O.R.C.A. Desktop 3.0.8

## 简体中文

- Modern 输入框与正文共用 884px 最大宽度，在聊天区域居中；滚动条仍贴聊天区域右缘。
- 底部控件保持单行：左侧添加与权限，中间运行状态，右侧思考强度、模型、暂停/恢复及发送/停止。权限继续使用单个菜单，未修改授权规则。
- 选择器默认透明，悬停与键盘聚焦时轻微突出；运行按钮使用中性色并保留稳定点击区域。未知模型强度显示“模型默认”，提供供应商设置入口。
- 回合定位短线在少量对话时居中，长会话内部滚动；修复恢复旧回合后的定位偏移，以及窄窗口审批区占用过多高度。
- 保留 Classic、会话、草稿和模型设置。延续 3.0.7 的安装器进程检查及关闭逻辑，手动升级前请保存任务。托管本地 AI 与电脑操控继续暂时禁用。
- GitHub 与 Mac 更新源提供相同签名包。Minisign 是更新完整性签名，不是 Windows 发布者签名；Windows 包仍未作 Authenticode 签名，macOS 包仍未公证。

## English

- Modern shares an 884px maximum width between the Composer and reading column. The scrollbar remains at the chat pane's right edge.
- Keep bottom controls on one row: add and access on the left, progress in the flexible center, then effort, model, pause/resume and send/stop on the right. Access remains a single menu with unchanged authorization rules.
- Use transparent selectors with subtle hover/focus feedback and neutral run buttons with stable hit areas. Models without declared effort levels show "Model default" and a provider-settings entry.
- Center the short turn rail, scroll long histories within its height limit, fix restored-history jump drift, and prevent narrow approval panels from consuming excess height.
- Preserve Classic, conversations, drafts and model settings. Retain the installer process checks and closure introduced in 3.0.7; save tasks before manual upgrades. Managed local AI and Computer Use remain disabled.
- GitHub and the Mac update source distribute identical signed packages. Minisign authenticates updates, not OS publisher identity. Windows remains Authenticode-unsigned and macOS remains unnotarized.

[Verification / 验收记录](../audits/desktop-v3.0.8-verification.md)
