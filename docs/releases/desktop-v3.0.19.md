# O.R.C.A. Desktop 3.0.19

## English

This release rebuilds the two things that made the built-in browser feel unlike a browser: the panel it lives in, and what it can actually do to a page.

**A side dock with tabs.** The right-hand panel is now a dock that holds tabs. A tab can be a side conversation, a file panel, the changed-files list, or a browser page, and several can be open at once. Opening a file or a page from a conversation puts it in its own tab instead of replacing whatever was already there; closing a tab returns you to its neighbour. The dock and the overview panel exclude each other: while the dock is open the overview is put away, and collapsing the dock brings the overview back.

**A browser that behaves like one.** The engine was already real WebView2; what failed was the layer above it.

- Pages that keep changing — timers, ads, animations, streaming text — no longer make every action fail as "stale". Staleness is now judged on the element being acted on: if the button you approved has been re-rendered with different text or state, the action is refused and the model re-reads the page; a page that merely moved on in the background does not block anything.
- Actions exist for the things real pages need: hover, double-click, clear a field, wait for a selector or a piece of text to appear, upload a file through a file input, and key presses (Enter, Tab, Escape, arrows, Home/End, PageUp/PageDown, Space) with modifiers.
- Observation reaches into open shadow roots and same-origin frames, so components built from custom elements are visible and operable, not just the outer document.
- JavaScript dialogs (`alert`, `confirm`, `prompt`) are answered automatically and recorded as events, instead of leaving the page waiting on a dialog nobody can see.
- Browser tools no longer disappear from the model's tool list when the window rebuilds its controller (switching model, mode or effort); that detachment is what produced `unknown tool "browser_task"`.

**Full access means full access.** With the permission selector on full access, browser actions run without a per-action prompt. In every other mode each action is still presented for approval, and password fields, card fields and one-time-code fields stay outside what the model may type into regardless of mode.

Verified against the public practice site `https://the-internet.herokuapp.com/`: fifteen scenarios covering forms, dropdowns, checkboxes, dynamic element insertion, key presses, JavaScript alerts, nested frames, iframes, shadow DOM, dynamically loaded content, hover menus, tables and navigation all pass through the same host the app ships.

See the [verification record](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.19/docs/audits/desktop-v3.0.19-verification.md) for the commands and their output.

The Windows build is unsigned unless the release was dispatched with the explicit unsigned override; macOS is not notarized. Minisign verifies update integrity only; it is not an operating-system publisher signature.

## 简体中文

这一版重做了两件让内置浏览器"不像浏览器"的事：它待的那个面板，以及它究竟能对一个页面做什么。

**带标签页的侧边坞。** 右侧面板现在是一个可以放多个标签的坞。标签可以是侧边对话、文件面板、改动列表，也可以是浏览器页面，并且可以同时开着好几个。在对话里打开一个文件或一个页面，它会自己占一个标签，而不是把原来的内容顶掉；关掉标签会回到相邻的那个。坞和概览面板互斥：坞展开时概览收起，坞回收之后概览自动伸回来。

**一个像浏览器的浏览器。** 引擎一直是真正的 WebView2，出问题的是它上面那一层。

- 一直在变的页面——计时器、广告、动画、流式文字——不再让每个动作都报"过期"。过期现在只看被操作的那个元素：如果你批准的那个按钮已经被重新渲染成别的文字或状态，动作会被拒绝、模型重新读页面；页面自己在后台继续变化不再卡住任何动作。
- 补齐真实页面需要的动作：悬停、双击、清空输入框、等待某个选择器或某段文字出现、通过文件框上传、以及按键（Enter、Tab、Escape、方向键、Home/End、PageUp/PageDown、空格）与组合键。
- 观察能进入开放的 shadow root 和同源 frame，用自定义元素写的组件看得见、也点得动，而不只是外层文档。
- JavaScript 对话框（`alert`、`confirm`、`prompt`）自动应答并记成事件，不会再把页面挂在一个人根本看不见的弹窗上。
- 窗口重建控制器（切模型、切模式、切思考强度）时，浏览器工具不再从工具表里消失；之前消失正是 `unknown tool "browser_task"` 的来源。

**完全访问就是完全访问。** 权限选择器选「完全访问」时，浏览器动作不再逐条弹确认；其他模式下每一步仍然先批准。无论哪种模式，密码框、银行卡号框和一次性验证码框都不允许模型输入。

已用公开练习站 `https://the-internet.herokuapp.com/` 验证：表单、下拉框、复选框、动态插入元素、按键、JavaScript 弹窗、嵌套 frame、iframe、shadow DOM、动态加载、悬停菜单、表格与导航共十五个场景，全部通过应用实际使用的同一个宿主跑通。

命令与输出见[验收记录](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.19/docs/audits/desktop-v3.0.19-verification.md)。

Windows 包未作 Authenticode 发布者签名（除非发布时显式启用未签名构建开关）；macOS 未公证。Minisign 仅验证更新完整性，不是操作系统发布者签名。
