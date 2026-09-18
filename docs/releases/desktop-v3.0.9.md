# O.R.C.A. Desktop 3.0.9

## 简体中文

- Windows 安装器恢复标准 NSIS / MUI2 向导：欢迎、许可、安装目录、安装选项、进度和完成。保留品牌、中文字体、桌面快捷方式选项与默认折叠的详细日志；不改变主应用 Modern / Classic 布局。
- 用随包分发的原生 Go 检查程序替代安装时的 PowerShell 进程检测。无目标进程且文件可替换时立即继续，不固定等待 5 秒。
- 正常退出最多等待 5 秒，随后仅结束已确认属于所选安装目录的目标残留；退出后立即继续，强制结束后的等待上限为 2 秒。复核路径、启动时间、进程句柄和父进程关系，避免误结束其他安装副本或无关 Node。
- 支持新版退出通道的应用在真正退出前保存草稿、附件与会话状态，避免普通关闭仅隐藏到后台。强制结束无响应旧版本无法保证保存尚未落盘的内容，手动升级前请保存任务；已落盘的配置、会话和模型不被清理。
- 分别报告仍在运行、外部文件占用、目录不可写和检测失败；提供受影响文件、占用者及日志信息，可重试、返回修改目录或取消。外部占用者不会被自动关闭；WebView2 准备后、覆盖文件前再次检查，继续禁止跳过安装文件。
- 保留助手、编程、ORCA Agent、供应商、图片附件、办公产物和 3.0.8 界面改进。托管本地 AI 与电脑操控继续在所有平台暂时禁用，不重复 3.0.5 已完成的官方模型迁移。
- 预览 CI [35320328576](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35320328576) 已通过隔离 Windows x64 全新安装、从 3.0.8 升级、默认卸载、有效载荷摘要及合成配置/会话/草稿/模型数据保留检查。这不替代正式构建验收；正式结果以验收记录为准。
- 人工查看覆盖预览包安装前页面与取消退出，未逐页验收进度、完成和卸载页面，也未完成真实 Wails 窗口带草稿退出的全流程人工操作。不宣称 Windows ARM64、macOS 或 Linux 安装 UI 已实测；草稿保存握手测试与合成数据保留不等于所有真实工作负载均已验收。
- Windows 包未作 Authenticode 发布者签名，macOS 包未公证。Minisign 验证更新完整性，不等于操作系统发布者认证。

## English

- Restore the standard Windows NSIS / MUI2 wizard: welcome, license, installation directory, options, progress, and completion. Retain branding, Chinese fonts, the desktop-shortcut option, and collapsed detail logs. The application's Modern / Classic layouts are unchanged.
- Replace installation-time PowerShell process detection with a bundled native Go helper. Continue immediately when no target process is running and files are replaceable, without a fixed 5-second delay.
- Allow up to 5 seconds for graceful exit, then terminate only confirmed target remnants belonging to the selected installation directory. Continue as soon as they exit, with at most 2 additional seconds of waiting after termination. Recheck paths, creation times, process handles, and parent relationships to avoid terminating other installations or unrelated Node processes.
- Applications supporting the new shutdown channel save drafts, attachments, and session state before actually exiting, instead of merely hiding in the background. Forced termination of an unresponsive older version cannot guarantee preservation of unsaved content; save tasks before manual upgrades. Persisted configuration, sessions, and models are not cleared.
- Report running targets, external file locks, unwritable directories, and detection failures separately, with affected files, lock owners, and log information. Offer retry, directory changes, or cancellation. Do not automatically close external lock owners. Recheck after WebView2 preparation and before replacing files; skipped installation files remain prohibited.
- Retain Assistant, Coding, ORCA Agent, providers, image attachments, office artifacts, and the 3.0.8 UI improvements. Managed local AI and Computer Use remain temporarily disabled on all platforms; completed 3.0.5 official-model migration is not repeated.
- Preview CI [35320328576](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35320328576) passed isolated Windows x64 fresh-install, upgrade-from-3.0.8, default-uninstall, payload-hash, and synthetic configuration/session/draft/model retention checks. This does not replace formal-build acceptance; see the verification record for formal results.
- Manual inspection covered pre-installation pages and cancellation in the preview package, not every progress, completion, or uninstall page, nor the complete draft-saving exit flow in a real Wails window. Windows ARM64, macOS, and Linux installer UIs are not claimed as tested. Draft-save handshake tests and synthetic data retention do not establish acceptance for every real workload.
- Windows packages lack Authenticode publisher signing; macOS packages are not notarized. Minisign verifies update integrity, not operating-system publisher identity.

[Release / 发布页](https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/desktop-v3.0.9) · [Verification / 验收记录](../audits/desktop-v3.0.9-verification.md) · [Build checklist / 构建清单](../build/desktop-v3.0.9.md)
