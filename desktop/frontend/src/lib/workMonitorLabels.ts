const en = {
  name: "Work monitor", close: "Close monitor", resize: "Resize monitor", request: "Request", partial: "Partial", details: "Details",
  unavailable: "This backend does not support monitoring. No data captured.", expired: "Capture stopped: subscription expired or tab changed.", failed: "Capture disconnected; subscription released.", subscribeFailed: "Cannot subscribe to the active tab.",
  limits: "Partial observation: authentication fields, recognizable keys, image data and original reasoning replay are filtered. Arbitrary secrets, especially across streaming chunks, cannot be guaranteed detectable. Scan: 8 MiB/request; retained JSON: 256 KiB; strings: 16 KiB. Content beyond a limit is incomplete. Only post-Hook response events are shown; prefill and Hook-internal time are not observable. Responses without request IDs are turn-scoped, not paired to an attempt. Tool dispatch does not confirm authorization or execution. Lane counts cover retained events only.",
  loss: "Incomplete records", backend: "backend evicted", dropped: "dropped", local: "local evicted", empty: "No observed request. Earlier data is not backfilled.",
  responses: "Response / tools", recent: "Showing latest 100 blocks", timeline: "Phase timeline", noPhase: "No phase events", follow: "Follow output", followTimeline: "Follow timeline", turnLevel: "Turn-level; request unavailable", tools: "tools", children: "children", observed: "observed active", trimmedTimeline: "Earlier phases omitted from view", filtered: "Filtered / limited",
};
const zh: typeof en = {
  name: "工作监控", close: "关闭工作监控", resize: "调整监控高度", request: "请求", partial: "部分", details: "详情",
  unavailable: "当前后端不支持监控，未采集数据。", expired: "采集已停止：订阅失效或标签已切换。", failed: "采集连接中断，订阅已释放。", subscribeFailed: "无法订阅当前标签。",
  limits: "局部观测：认证字段、可识别密钥、图片数据和原始推理重放已过滤；无法保证识别所有秘密，尤其是跨流式片段的秘密。每请求扫描 8 MiB，保留 JSON 256 KiB，字符串 16 KiB，超限标为不完整。仅显示 Hook 后响应，prefill 与 Hook 内部耗时不可观测。缺少请求 ID 的响应仅关联回合，不强行归到某次尝试。工具分派不代表已获授权或执行；并行计数仅涵盖保留的事件。",
  loss: "记录不完整", backend: "后端淘汰", dropped: "丢弃", local: "本地淘汰", empty: "尚未观察到请求，打开前的数据不回填。",
  responses: "响应 / 工具", recent: "仅展示最近 100 个记录块", timeline: "阶段时间线", noPhase: "暂无阶段事件", follow: "跟随输出", followTimeline: "跟随时间线", turnLevel: "回合级，缺少请求关联", tools: "工具", children: "子任务", observed: "已观测活跃", trimmedTimeline: "更早阶段未在视图展示", filtered: "已过滤 / 限制",
};
export const workMonitorLabels = (locale: string) => locale === "zh" ? zh : en;
const phasesEN: Record<string, string> = { input: "Input", "wait-first": "Waiting for first chunk", reasoning: "Reasoning", decode: "Output", tool: "Tool", wait: "Waiting", final: "Final", paused: "Paused", cancelling: "Cancelling", stopped: "Stopped", error: "Error" };
const phasesZH: Record<string, string> = { input: "输入", "wait-first": "等待首片段", reasoning: "推理", decode: "输出", tool: "工具", wait: "等待", final: "最终答复", paused: "已暂停", cancelling: "正在取消", stopped: "已停止", error: "错误" };
export const workMonitorPhase = (phase: string, locale: string) => (locale === "zh" ? phasesZH : phasesEN)[phase] ?? phase;
