# O.R.C.A. Desktop 3.0.12

## 简体中文

本次补丁修复回合结束检查误判及流式请求的生命周期问题，不重做界面，不更改供应商或模型配置。

- 待办未完成时，已恢复的工具失败、被拒绝的清单更新不再被当作新的致命动作失败。未完成事项保持原样，真实未恢复失败仍会提示或拦截。
- 结束检查改为简短中英文提示；旧诊断仅改善显示，不改写历史状态。
- 收到流式结束信号后及时释放请求；错误和取消不会让已完成请求继续占用连接。
- 有界缓冲吸收短暂消费延迟，持续阻塞则关闭响应并明确报错，不自动重放已经收到的输出或工具。网络空闲判断使用真实字节活动，兼容拆分到多次读取的 SSE 行。
- 保留 3.0.11 压缩、输入框、图片粘贴和加载改进，标准安装向导、Modern / Classic 布局及自定义供应商保持不变。

源码压缩包单独提供，可作为另一个项目的起点；请参阅包内 `docs/SOURCE_REUSE.md`，保留 MIT 与第三方许可，并为新项目更换更新源、签名密钥、存储路径和应用标识。

测试使用合成内容，没有读取用户聊天记录。完整原生窗口交互未验收；安装升级在隔离 CI 中验证，不在用户工作电脑上安装。Windows 未作 Authenticode 签名，macOS 未公证；Minisign 用于更新真实性校验。

## English

This patch fixes end-of-turn readiness false positives and stream lifecycle defects. It does not redesign the UI or change provider/model configuration.

- Recovered tool failures and rejected checklist updates no longer turn remaining todos into a fatal action failure. Unfinished items remain open; real unresolved failures retain their checks.
- Readiness notices are concise and localized. Older diagnostics get a display-only explanation without rewriting stored outcomes.
- Completion, errors, and cancellation release the request promptly instead of leaving its connection alive.
- Bounded buffering absorbs short consumer delays. Sustained backpressure closes the response with an explicit error without replaying partial output or tools. Network idleness follows actual received bytes, including fragmented SSE lines.
- Retains 3.0.11 compaction, Composer, paste, and loading improvements, the standard installer, Modern / Classic, and custom providers.

A separate source archive is provided for reuse. Read `docs/SOURCE_REUSE.md`; keep MIT and third-party notices, and replace update origins, signing keys, storage locations, and application identities for a new project.

Tests use synthetic content, not user conversations. Full native-window interaction is not certified; installer acceptance runs in isolated CI, not on the user's workstation. Windows is not Authenticode-signed and macOS is not notarized; Minisign authenticates updates.

[Verification / 验收](../audits/desktop-v3.0.12-verification.md) · [Build / 构建](../build/desktop-v3.0.12.md)
