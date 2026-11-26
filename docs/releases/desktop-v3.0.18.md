# O.R.C.A. Desktop 3.0.18

## English

This release turns the desktop's browser permission, account sign-in and model list into things that follow the app rather than a one-time setup.

- Browser actions now follow the permission selector at the bottom of the window. With full access chosen there, the built-in browser opens pages and acts without a per-action prompt; in every other mode each action is still presented for approval.
- WhaleStack sign-in moved to the default browser. The app shows a one-time code, the browser confirms the account, and the app comes back signed in — the password is never typed into this process.
- The account capsule in the top-left is now a visible blue control instead of a faint label, and it names the signed-in account.
- The model picker names the model the local deployment is actually running, not a frozen alias: the relay keeps one stable call name (`lens-main`) while the display name, context window and vision support are re-read from the gateway, so swapping the loaded weight renames every picker without re-signing in or copying a new key. Sessions saved against the old name still resolve.
- The model list is no longer frozen at sign-in. It is re-read when the app starts and on demand from the capsule, so swapping the model served by the local deployment reaches every picker without signing in again. A failed refresh keeps the last known list.
- Rendering work is deferred: the settings, history, automation and tool-library panels load on first open, and background polling slows down or stops while the window is hidden.

Sign-in now completes against the current account service: the login service mints the relay key through the unified key API, bound to the local-deployment group, and reuses the account's existing desktop key on later sign-ins.

See the [verification record](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.18/docs/audits/desktop-v3.0.18-verification.md) for build and deployment evidence.

The Windows installer is unsigned unless the release was dispatched with the explicit unsigned override; macOS is not notarized. Minisign verifies update integrity only; it is not an operating-system publisher signature.

## 简体中文

本次把桌面端的浏览器权限、账号登录和模型列表都改成「跟着应用走」，而不是一次性设置。

- 浏览器动作跟随窗口下方的权限选择器：选了完全访问，内置浏览器可以直接打开页面并操作，不再逐次确认；其他模式下每一步动作仍然先弹给你批准。
- 鲸栈登录改为默认浏览器授权：应用显示一次性验证码，浏览器里确认账号，批准后自动回到应用，密码不经过这个进程。
- 左上角的账号胶囊改成可见的蓝色控件，并显示当前登录的账号名。
- 模型选择器显示本地部署**当前实际加载**的模型名，而不是写死的别名：中转站保留固定调用名 `lens-main`，显示名、上下文长度与视觉支持从网关实时读取，所以换权重后各选择器直接改名，不用重新登录、也不用换 Key。之前按旧名保存的会话仍能正常解析。
- 模型列表不再冻结在登录那一刻：应用启动时重新读取，也可以从胶囊里手动刷新。本地部署换了模型，所有选择器下次打开就是新的；刷新失败时保留上一份列表，不会清空。
- 渲染做了延后：设置、历史、自动化和工具库面板首次打开才加载，窗口隐藏时后台轮询降频或暂停。

登录现在可以在当前的账号服务上走完：登录服务通过 unified 密钥接口签发中转密钥并绑定到本地部署分组，同一账号再次登录会复用已有的桌面端密钥。

构建与部署证据见[验收记录](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.18/docs/audits/desktop-v3.0.18-verification.md)。

Windows 安装器未签名（除非发布时显式启用未签名构建开关）；macOS 未公证。Minisign 仅验证更新完整性，不是操作系统发布者签名。
