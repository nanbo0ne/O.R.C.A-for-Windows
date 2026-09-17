# O.R.C.A. Desktop 3.0.7

## 简体中文

- 修复 Windows 32 位安装器无法识别 64 位后台进程的问题。
- 安装前尝试关闭目标目录的旧程序，等待 30 秒后自动结束残留进程，再检查文件是否可写。请在手动升级前保存任务。
- 无法确认进程路径、文件被占用或无写入权限时，阻止覆盖。禁止跳过安装文件，避免新旧文件混装。
- 应用内升级继续先检查活动任务并保存会话。配置、会话、模型文件和 Classic 保留。
- GitHub 与 Mac 更新源使用同一组签名文件。Minisign 是更新完整性签名，不是 Windows 发布者签名。

## English

- Fix detection of 64-bit background processes from the 32-bit Windows installer.
- Before replacement, request closure of the target installation, allow 30 seconds for shutdown, stop remaining target processes, and check file access. Save tasks before manual installation.
- Block replacement when process paths cannot be verified or target executables are locked or not writable. Installation files cannot be skipped.
- In-app updates still check active work and save sessions first. Configuration, conversations, model files, and Classic are retained.
- GitHub and the Mac update source distribute identical signed assets. Minisign authenticates updates; it is not Windows publisher signing.
