# O.R.C.A. Desktop 3.0.5

**3.0.5 更新说明 / Release notes.** [实测记录 / Verification](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.5/docs/audits/desktop-v3.0.5-verification.md).

## 简体中文

O.R.C.A. 保留助手、编程与 ORCA Agent 三种模式，以及 Modern / Classic 界面。完整产品介绍见[中文 README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.5/README.md)。

### 模型与思考

- 默认官方模型升级为 **DeepSeek V4.1 Flash**，规范引用 `deepseek/deepseek-flash`，API 名称 `deepseek-flash`。
- 支持原生视觉、1M token 上下文、最大 384K token 输出、工具调用与 JSON Output。最大输出不等于每次请求的默认预算。
- `deepseek-v4-flash` 与 `deepseek-v4-flash-vision-exp` 成为 Flash 兼容别名，使用相同价格；`deepseek/deepseek-v4-pro` 继续可选，仍为纯文本模型。
- 思考强度提供 `low` / `high` / `max`，`auto` 使用 `high`。思考开关独立于强度，简略/详细仅改变思考内容的显示。
- 工具调用续传与会话恢复保留已捕获的思考内容；不为缺失的旧记录编造思考内容。服务端思考历史错误会明确显示，不触发工具重放。

### 升级与费用

- 新安装使用 Flash；已有官方 DeepSeek 默认值、角色和保存的会话模型选择一次性迁移到 Flash，包括此前的官方 Pro 选择。迁移后可重新选择 Pro，后续启动保留该选择。
- 自定义供应商、代理端点、独立凭据及非官方模型选择保留；历史消息、会话 ID、附件和已存费用不重写。升级前请备份配置与会话。
- 人民币价格见下方共用表，Flash 别名同价。峰时为固定 UTC+8 周一至周五 09:00-12:00、14:00-18:00，其余时间含周末为闲时。
- 每次请求开始时冻结计价依据；历史 USD/CNY 金额保留原币种，不重算、不改标，也不合并为一个权威总额。

### 平台说明

- 托管本地 AI 与 Computer Use 在 Windows、macOS、Linux 继续禁用，代码、配置和模型文件保留。外部兼容本地服务与普通图片附件仍可使用；原生视觉不启用截屏、鼠标、键盘或窗口控制。
- Windows 沿用无 Authenticode 签名政策，macOS 未公证，系统可能显示信任提示。Minisign 用于验证更新来源和完整性，不替代操作系统的发布者信任。
- 保留现有安装兼容性，不生成旧品牌重复资产；本版不包含 DPI 调整。

## English

O.R.C.A. retains Assistant, Coding, and ORCA Agent modes, with Modern and Classic interfaces. See the [English README](https://github.com/nanbo0ne/O.R.C.A-for-Windows/blob/desktop-v3.0.5/README.en.md) for the full product.

### Models and Reasoning

- The official default becomes **DeepSeek V4.1 Flash**: canonical reference `deepseek/deepseek-flash`, API name `deepseek-flash`.
- Flash supports native vision, a 1M-token context, up to 384K output tokens, tool calls, and JSON Output. Maximum output is not the default request budget.
- `deepseek-v4-flash` and `deepseek-v4-flash-vision-exp` become Flash compatibility aliases at the same prices. `deepseek/deepseek-v4-pro` remains selectable and text-only.
- Reasoning effort offers `low` / `high` / `max`, with `auto` resolving to `high`. The thinking toggle is separate from effort; Compact/Detailed only changes reasoning display.
- Tool continuations and restored sessions retain captured reasoning. Missing historical reasoning is never invented; provider reasoning-history errors surface explicitly without replaying tools.

### Upgrades and Billing

- New installations use Flash. Existing official DeepSeek defaults, roles, and saved session model choices migrate once to Flash, including previous official Pro choices. Selecting Pro afterward survives later startups.
- Custom providers, proxy endpoints, separate credentials, and non-official model choices remain intact. Historical messages, session IDs, attachments, and stored costs are preserved. Back up configuration and sessions before upgrading.
- The shared table below lists RMB rates; Flash aliases use the same prices. Peak hours are Monday-Friday 09:00-12:00 and 14:00-18:00 in fixed UTC+8; all other times, including weekends, are off-peak.
- Pricing is fixed at request start. Historical USD/CNY amounts retain their currencies without recalculation, relabeling, or a combined authoritative total.

### Platform Notes

- Managed local AI and Computer Use remain disabled on Windows, macOS, and Linux; code, configuration, and model files remain. External compatible local services and ordinary image attachments stay available. Native vision does not enable screen capture or mouse, keyboard, or window control.
- Windows follows the existing policy without Authenticode signing; macOS is unnotarized. OS trust warnings may appear. Minisign verifies update origin and integrity; it does not replace OS publisher trust.
- Existing installer compatibility is retained, with no old-brand duplicate assets. This version includes no DPI changes.

## 人民币价格 / RMB Pricing

单位 / Unit: CNY per million tokens. 2026-09-17 核对 / Checked September 17, 2026.

| 模型与时段 / Model and period | 缓存命中输入 / Cache-hit input | 未命中输入 / Cache-miss input | 输出 / Output |
| --- | ---: | ---: | ---: |
| V4.1 Flash 闲时 / off-peak | 0.02 | 1 | 4 |
| V4.1 Flash 峰时 / peak | 0.04 | 2 | 8 |
| V4 Pro 闲时 / off-peak | 0.15 | 4.5 | 13.5 |
| V4 Pro 峰时 / peak | 0.30 | 9 | 27 |

## 官方来源 / Official Sources

- [模型与价格 / Models and pricing](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)
- [更新日志 / Updates](https://api-docs.deepseek.com/zh-cn/updates)
- [思考模式 / Thinking mode](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/)
