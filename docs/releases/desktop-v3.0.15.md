# O.R.C.A. Desktop 3.0.15

## English

This release adds WhaleStack account sign-in to the provider settings. Enter a WhaleStack username (or verified email) and password once and the app registers a provider named 鲸栈 pointing at the relay endpoint, with the account's model list already filled in — no Base URL or API key to type in.

- The relay key is derived per account by the login service and stored as `WHALESTACK_API_KEY`; signing in again from any machine reuses the same key instead of creating duplicates.
- A wrong password, an account without model access, or an unreachable relay is reported inside the card, not as a generic banner.
- Signing in again updates the existing provider rather than adding a second one.
- The conversation, work-overview, and permission-dialog improvements from 3.0.13 remain.
- The Linux sandbox tests now accept bubblewrap when it is installed, so the release gate passes on Linux runners.

See the [verification record](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.15/docs/audits/desktop-v3.0.15-verification.md) for build and deployment evidence.

The Windows installer is unsigned unless the release was dispatched with the explicit unsigned override; macOS is not notarized. Minisign verifies update integrity only; it is not an operating-system publisher signature.

## 简体中文

本次新增「用鲸栈账号登录」。在设置里填入鲸栈用户名（或已验证邮箱）和密码，应用会自动注册一个名为「鲸栈」的供应商，指向中转端点并载入该账号可用的模型，不需要手动填写 Base URL 或 API Key。

- 中转 Key 由登录服务按账号派生并保存在 `WHALESTACK_API_KEY`；在任意机器重新登录都会复用同一把 Key，不会生成重复 Key。
- 密码错误、账号未开通模型权限或中转服务不可达时，原因直接显示在卡片内，而不是笼统的横幅提示。
- 再次登录会更新已有供应商，不会重复添加。
- 3.0.13 的会话记录、工作进度概览与权限窗口改进全部保留。
- Linux 沙箱测试改为按 `bwrap` 是否存在判断实际行为，发布门禁可以在 Linux runner 上通过。

构建与部署证据见[验收记录](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.15/docs/audits/desktop-v3.0.15-verification.md)。

Windows 安装器未签名（除非发布时显式启用未签名构建开关）；macOS 未公证。Minisign 仅验证更新完整性，不是操作系统发布者签名。

[Verification / 验收记录](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.15/docs/audits/desktop-v3.0.15-verification.md) · [Build / 构建](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.15/docs/build/desktop-v3.0.15.md)
