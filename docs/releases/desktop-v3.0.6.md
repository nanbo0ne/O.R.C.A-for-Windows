# O.R.C.A. Desktop 3.0.6

**3.0.6 更新说明 / Release notes.** [验收记录 / Verification](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/main/docs/audits/desktop-v3.0.6-verification.md) · [构建与分发 / Build and delivery](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/main/docs/build/desktop-v3.0.6.md).

## 简体中文

- **Modern 导航：** 修正聊天区导航留白与紧凑回合导航条，避免挤占正文；无布局重设计，Classic 保持不变。
- **工具摘要：** 合并连续工具摘要，保留真实阶段文本及其顺序，不用通用状态替换，不吞掉中间回复；最终回答仍独立呈现。
- **自定义供应商：** 修正界面显示 OpenAI-compatible 却保存按字母排序首位的 Anthropic 注册类型的问题，显示协议与保存值保持一致。
- **Agent 端点：** 后端规范化请求端点；协议与端点不匹配时给出可操作的错误，提示检查协议类型与 Base URL，保留供应商身份及独立凭据边界。
- **升级边界：** 安装验收目标为已发布 3.0.5 至 3.0.6。此补丁不新增或重复 3.0.5 已完成的官方 DeepSeek 迁移，之后主动选择的 Pro 与自定义模型继续保留。
- 助手、编程、ORCA Agent、图片与文件、产物、工具、记忆和自动化继续保留。托管本地 AI 与 Computer Use 在所有平台保持禁用；实现、配置和模型文件保留，外部兼容本地服务与普通图片附件不受此禁用影响。

本地模型实测已复现错误的 Anthropic 路由 400；使用 OpenAI 协议的短文本和工具往返通过。已有误存接入请在设置中将协议改为 OpenAI-compatible，并核对当前 Base URL、模型名和 Key；不会批量覆盖已有 Anthropic 配置。完整产品、安装与平台限制见[中文 README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.6/README.md)。

## English

- **Modern navigation:** correct the chat navigation gutter and compact turn rail without crowding the transcript. No layout redesign; Classic is unchanged.
- **Tool summaries:** merge consecutive summaries while retaining actual stage text and order, without generic status replacements or lost intermediate replies. Final answers remain separate.
- **Custom providers:** correct the UI showing OpenAI-compatible while saving the alphabetically first Anthropic registration. The displayed protocol and saved value must agree.
- **Agent endpoints:** normalize backend request endpoints and surface actionable protocol/endpoint mismatch errors that identify the protocol and Base URL to check. Provider identity and separate credential boundaries remain intact.
- **Upgrade boundary:** installer acceptance targets published 3.0.5 to 3.0.6. This patch introduces no new official DeepSeek migration and does not repeat completed 3.0.5 migration; subsequent deliberate Pro and custom-model selections remain.
- Assistant, Coding, ORCA Agent, images and files, artifacts, tools, memory, and automation remain. Managed local AI and Computer Use stay disabled on all platforms, retaining implementations, configuration, and model files. External compatible local services and ordinary image attachments are unaffected by these disabled features.

Live local-model tests reproduced the unsupported Anthropic route's HTTP 400; OpenAI text and tool-continuation requests passed. For an access entry saved with the wrong protocol, choose OpenAI-compatible and verify its current Base URL, model ID and key. Existing Anthropic configurations are not bulk-rewritten. See the [English README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.6/README.en.md) for the full product, installation and platform limits.
