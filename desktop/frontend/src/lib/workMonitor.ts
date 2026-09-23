export interface MonitorEntry {
  generation: string; seq: number; seqEnd?: number; tabId: string; turnId: string; requestId?: string; attemptId?: string;
  time: number; kind: string; phase?: string; data?: unknown; incomplete?: boolean;
}
export interface MonitorSnapshot {
  generation: string; entries: MonitorEntry[]; cursor: number;
  evicted: number; dropped: number; bytes: number; expired: boolean;
}
export interface MonitorState {
  generation: string; entries: MonitorEntry[]; cursor: number; localEvicted: number;
  evicted: number; dropped: number; bytes: number;
}
export const emptyMonitor = (generation = ""): MonitorState => ({ generation, entries: [], cursor: 0, localEvicted: 0, evicted: 0, dropped: 0, bytes: 0 });
// UTF-16 plus conservative object overhead; keep the webview below 4 MiB.
export const monitorCost = (entry: MonitorEntry) => JSON.stringify(entry).length * 2 + 512;
export function reduceMonitor(state: MonitorState, snapshot: MonitorSnapshot, tabId: string): MonitorState {
  if (snapshot.generation !== state.generation || snapshot.expired) return state;
  let entries = [...state.entries];
  let bytes = state.bytes;
  let localEvicted = state.localEvicted;
  let cursor = state.cursor;
  for (const entry of snapshot.entries ?? []) {
    if (entry.generation !== state.generation || entry.tabId !== tabId || entry.seq <= cursor) continue;
    cursor = entry.seq;
    const previous = entries[entries.length - 1];
    const data = entry.data as { channel?: string; text?: string } | undefined;
    const prevData = previous?.data as { channel?: string; text?: string } | undefined;
    if (entry.kind === "response" && previous?.kind === "response" && entry.turnId === previous.turnId && entry.requestId === previous.requestId
      && data?.channel === prevData?.channel && typeof data?.text === "string" && typeof prevData?.text === "string" && data.text.length + prevData.text.length <= 65536) {
      const merged = { ...previous, seqEnd: entry.seq, incomplete: previous.incomplete || entry.incomplete, data: { ...prevData, text: prevData.text + data.text } };
      bytes += monitorCost(merged) - monitorCost(previous);
      entries[entries.length - 1] = merged;
    } else { entries.push(entry); bytes += monitorCost(entry); }
  }
  let requests = entries.filter(e => e.kind === "request").length;
  while (entries.length && (bytes > 4 * 1024 * 1024 || entries.length > 4096 || requests > 100)) {
    const removed = entries.shift()!;
    bytes -= monitorCost(removed); localEvicted++;
    if (removed.kind === "request") requests--;
  }
  return { generation: state.generation, entries, cursor: Math.max(cursor, snapshot.cursor), localEvicted, evicted: snapshot.evicted, dropped: snapshot.dropped, bytes };
}
export const phaseLabels: Record<string, string> = {
  input: "输入", "wait-first": "等待首片段", reasoning: "推理", decode: "输出",
  tool: "工具", wait: "等待", final: "完成", paused: "已暂停", cancelling: "正在取消", stopped: "已停止", error: "错误",
};
export function monitorTimeline(entries: MonitorEntry[]): MonitorEntry[] {
  return entries.filter(entry => Boolean(entry.phase));
}
export function clampMonitorHeight(height: number, viewport: number): number {
  return Math.max(0, Math.min(Math.max(140, Math.round(height)), 620, Math.floor(viewport * 0.65)));
}
export const monitorViewportFits = (height: number) => height >= 560;

export const terminalPhase = (phase?: string) => phase === "final" || phase === "stopped" || phase === "error";
export function phaseDuration(entry: MonitorEntry, next: MonitorEntry | undefined, now: number): number {
  return Math.max(0, (next?.time ?? (terminalPhase(entry.phase) ? entry.time : now)) - entry.time);
}
export function observedLanes(entries: MonitorEntry[]): { tools: number; children: number } {
  const tools = new Set<string>(), children = new Set<string>();
  for (const entry of entries) {
    if (terminalPhase(entry.phase)) { for (const id of tools) if (id.startsWith(`${entry.turnId}/`)) tools.delete(id); }
    const data = entry.data as { id?: string; kind?: string; running?: boolean } | undefined;
    if (!data?.id) continue;
    if (entry.kind === "tool") {
      const id = `${entry.turnId}/${data.id}`;
      if (data.kind === "tool_result") tools.delete(id); else tools.add(id);
    }
    if (entry.kind === "child") { if (data.running) children.add(data.id); else children.delete(data.id); }
  }
  return { tools: tools.size, children: children.size };
}
export const nearEnd = (extent: number, offset: number, viewport: number, tolerance: number) => extent - offset - viewport <= tolerance;

// Serialize subscription changes so a slow old subscribe cannot replace a newer pane.
let changes: Promise<unknown> = Promise.resolve();
export function monitorChange<T>(fn: () => Promise<T>): Promise<T> {
  const next = changes.then(fn, fn);
  changes = next.catch(() => {});
  return next;
}
