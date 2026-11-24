# O.R.C.A. Desktop 3.0.16

## English

This release brings the built-in browser from the beta line to the official build, alongside the WhaleStack account sign-in first prepared in 3.0.15.

- The model can open pages, read the visible state, click, type, and scroll through the built-in browser. Every action is presented for approval before it runs; there is no silent automation.
- Browser control is available on Windows. The page lives in its own WebView host next to the conversation, so the workspace and the page stay independent.
- Sign in with a WhaleStack account under Settings → Provider access: the app registers a provider named 鲸栈 pointing at the relay endpoint and fills in that account's model list. No Base URL or API key to type in.
- The relay key is derived per account by the login service and stored as `WHALESTACK_API_KEY`; signing in again from any machine reuses the same key.
- The conversation, work-overview, and permission-dialog improvements from 3.0.13 remain, and the Linux sandbox tests accept bubblewrap so the release gate passes on Linux runners.

See the [verification record](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.16/docs/audits/desktop-v3.0.16-verification.md) for build and deployment evidence.

The Windows installer is unsigned unless the release was dispatched with the explicit unsigned override; macOS is not notarized. Minisign verifies update integrity only; it is not an operating-system publisher signature.

## 简体中文

本次把 beta 线上的内置浏览器带进正式版，并保留 3.0.15 准备好的「用鲸栈账号登录」。

- 模型可以自己打开页面、读取可见状态、点击、输入和滚动，全部在内置浏览器里完成；每一步动作执行前都会弹给你批准，不会静默操作。
- 浏览器操控目前只在 Windows 上提供。页面跑在独立于配合会话的 WebView 宿主里，工作区和页面互不干扰。
- 在「设置 → 供应商接入」登录鲸栈账号：应用会自动注册一个名为「鲸栈」的供应商，指向中转端点并载入该账号可用的模型，不需要手动填写 Base URL 或 API Key。
- 中转 Key 由登录服务按账号派生并保存在 `WHALESTACK_API_KEY`；在任意机器重新登录都会复用同一把 Key，不会生成重复 Key。
- 3.0.13 的会话记录、工作进度概览与权限窗口改进全部保留；Linux 沙箱测试改为按 `bwrap` 是否存在判断实际行为，发布门禁可以在 Linux runner 上通过。

构建与部署证据见[验收记录](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.16/docs/audits/desktop-v3.0.16-verification.md)。

Windows 安装器未签名（除非发布时显式启用未签名构建开关）；macOS 未公证。Minisign 仅验证更新完整性，不是操作系统发布者签名。
