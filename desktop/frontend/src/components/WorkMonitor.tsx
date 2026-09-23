import { useEffect, useMemo, useRef, useState } from "react";
import { Activity, ArrowDown, Info, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useI18n } from "../lib/i18n";
import { clampMonitorHeight, emptyMonitor, monitorChange, monitorTimeline, monitorViewportFits, nearEnd, observedLanes, reduceMonitor } from "../lib/workMonitor";
import { workMonitorLabels, workMonitorPhase } from "../lib/workMonitorLabels";
import type { MonitorEntry } from "../lib/workMonitor";
import { PhaseDrum } from "./PhaseDrum";
import { phaseSector } from "../lib/phaseDrum";
import "./work-monitor.css";

function useMonitor(tabId: string) {
  const [state, setState] = useState(emptyMonitor);
  const [error, setError] = useState<"" | "unavailable" | "expired" | "failed" | "subscribeFailed">("");
  const [visible, setVisible] = useState(() => !document.hidden);
  useEffect(() => {
    const changed = () => setVisible(!document.hidden);
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, []);
  useEffect(() => {
    setState(emptyMonitor()); setError("");
    if (!visible || !tabId) return;
    if (!app.MonitorSubscribe || !app.MonitorSnapshot || !app.MonitorUnsubscribe) { setError("unavailable"); return; }
    let stopped = false, generation = "", cursor = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const unsubscribe = (id: string) => monitorChange(() => app.MonitorUnsubscribe!(id)).catch(() => {});
    const poll = async () => {
      if (stopped || document.hidden) return;
      try {
        const snapshot = await app.MonitorSnapshot!(generation, cursor);
        if (stopped || document.hidden) return;
        if (snapshot.expired) { setError("expired"); void unsubscribe(generation); return; }
        cursor = snapshot.cursor;
        setState(previous => reduceMonitor(previous, snapshot, tabId));
        timer = setTimeout(() => void poll(), 250);
      } catch {
        if (!stopped) setError("failed");
        void unsubscribe(generation);
      }
    };
    void monitorChange(() => app.MonitorSubscribe!(tabId)).then(snapshot => {
      generation = snapshot.generation;
      if (stopped || document.hidden) { void unsubscribe(generation); return; }
      setState(emptyMonitor(generation)); void poll();
    }).catch(() => { if (!stopped) setError("subscribeFailed"); });
    return () => { stopped = true; clearTimeout(timer); if (generation) void unsubscribe(generation); };
  }, [tabId, visible]);
  return { state, error };
}

function Payload({ data }: { data: unknown }) {
  if (data == null) return null;
  if (typeof data !== "object" || Array.isArray(data)) return <pre>{JSON.stringify(data, null, 2)}</pre>;
  return <dl className="work-monitor__fields">{Object.entries(data).map(([key, value]) => <div key={key}>
    <dt>{key}</dt><dd>{typeof value === "object" && value !== null
      ? <details><summary>{Array.isArray(value) ? `[${value.length}]` : "{...}"}</summary><Payload data={value} /></details>
      : <pre>{String(value ?? "null")}</pre>}</dd>
  </div>)}</dl>;
}

function Entry({ entry, locale }: { entry: MonitorEntry; locale: string }) {
  const l = workMonitorLabels(locale);
  const data = entry.data as { channel?: string; text?: string } | undefined;
  return <section className="work-monitor__entry">
    <header title={entry.requestId || l.turnLevel}>#{entry.seq}{entry.seqEnd ? `-${entry.seqEnd}` : ""} {data?.channel ? workMonitorPhase(data.channel, locale) : entry.kind} {entry.incomplete && <strong>{l.partial}</strong>}</header>
    {entry.kind === "response" ? <pre>{data?.text}</pre> : <Payload data={entry.data} />}
  </section>;
}

type WorkMonitorProps = { tabId: string; onClose: () => void };
export function WorkMonitor(props: WorkMonitorProps) {
  const [fits, setFits] = useState(() => monitorViewportFits(window.innerHeight));
  useEffect(() => {
    const resize = () => {
      const next = monitorViewportFits(window.innerHeight);
      setFits(next);
      if (!next) props.onClose();
    };
    resize();
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, [props.onClose]);
  return fits ? <WorkMonitorPane {...props} /> : null;
}

function WorkMonitorPane({ tabId, onClose }: WorkMonitorProps) {
  const { locale } = useI18n(), l = workMonitorLabels(locale);
  const { state, error } = useMonitor(tabId);
  const [selected, setSelected] = useState(0);
  const [height, setHeight] = useState(() => clampMonitorHeight(320, window.innerHeight));
  const [follow, setFollow] = useState(true);
  const drag = useRef<{ y: number; height: number } | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const requests = useMemo(() => state.entries.filter(e => e.kind === "request"), [state.entries]);
  const request = requests.find(e => e.seq === selected) ?? requests[requests.length - 1];
  const related = useMemo(() => state.entries.filter(e => e.kind !== "phase" && e.kind !== "request" && (request?.turnId ? e.turnId === request.turnId : true)
    && (!e.requestId || !request?.requestId || e.requestId === request.requestId)), [state.entries, request]);
  const shown = related.slice(-100);
  const phases = useMemo(() => monitorTimeline(state.entries), [state.entries]);
  const last = phases[phases.length - 1];
  const lastActivePhase = phases.reduce<string | undefined>((active, e) => phaseSector(e.phase) !== null ? e.phase : active, undefined);
  const lanes = useMemo(() => observedLanes(state.entries), [state.entries]);
  const loss = state.evicted + state.dropped + state.localEvicted;
  const incomplete = loss > 0 || state.entries.some(e => e.incomplete);
  const requestData = request?.data as { body?: Record<string, unknown> } | undefined;
  useEffect(() => {
    const resized = () => setHeight(h => clampMonitorHeight(h, window.innerHeight));
    window.addEventListener("resize", resized); return () => window.removeEventListener("resize", resized);
  }, []);
  useEffect(() => { if (follow && contentRef.current) contentRef.current.scrollTop = contentRef.current.scrollHeight; }, [state.cursor, follow]);
  return <section className="work-monitor" style={{ height }} aria-label={l.name}>
    <div className="work-monitor__resize" role="separator" aria-label={l.resize} aria-orientation="horizontal" tabIndex={0}
      aria-valuemin={140} aria-valuemax={clampMonitorHeight(620, window.innerHeight)} aria-valuenow={height}
      onPointerDown={e => { e.preventDefault(); drag.current = { y: e.clientY, height }; e.currentTarget.setPointerCapture(e.pointerId); }}
      onPointerMove={e => { if (drag.current) setHeight(clampMonitorHeight(drag.current.height + drag.current.y - e.clientY, window.innerHeight)); }}
      onPointerUp={() => { drag.current = null; }} onPointerCancel={() => { drag.current = null; }}
      onDoubleClick={() => setHeight(clampMonitorHeight(320, window.innerHeight))}
      onKeyDown={e => { if (e.key === "ArrowUp" || e.key === "ArrowDown") { e.preventDefault(); setHeight(h => clampMonitorHeight(h + (e.key === "ArrowUp" ? 16 : -16), window.innerHeight)); } }} />
    <header className="work-monitor__heading"><Activity size={14} /><span>{l.name}</span>
      <span className="work-monitor__badge" title={`${l.limits}\n${l.observed}: ${lanes.tools} ${l.tools}, ${lanes.children} ${l.children}`}>{incomplete ? l.partial : l.filtered}</span>
      <button type="button" title={l.close} aria-label={l.close} onClick={onClose}><X size={14} /></button>
    </header>
    <div className="work-monitor__content" ref={contentRef} onScroll={e => { const el = e.currentTarget; setFollow(nearEnd(el.scrollHeight, el.scrollTop, el.clientHeight, 48)); }}>
      {error && <p role="status">{l[error]}</p>}
      {loss > 0 && <p className="work-monitor__warning" role="status">{l.loss}: {l.backend} {state.evicted}, {l.dropped} {state.dropped}, {l.local} {state.localEvicted}</p>}
      {requests.length > 0 ? <>
        <select aria-label={l.request} value={request?.seq ?? 0} onChange={e => { setSelected(Number(e.target.value)); setFollow(false); }}>
          {requests.map(r => <option key={r.seq} value={r.seq}>#{r.seq} {new Date(r.time).toLocaleTimeString()} · {(r.data as {body?: {model?: string}})?.body?.model ?? l.request}</option>)}
        </select>
        <details className="work-monitor__metadata"><summary><Info size={11} /> {l.details}</summary>
          <p>{l.limits}</p><pre>{JSON.stringify({ tabId, turnId: request?.turnId, requestId: request?.requestId, attemptId: request?.attemptId, generation: state.generation }, null, 2)}</pre>
          <Payload data={requestData?.body && Object.fromEntries(Object.entries(requestData.body).filter(([key]) => key !== "messages" && key !== "system"))} />
        </details>
        {requestData?.body?.system != null && <details><summary>system</summary><Payload data={requestData.body.system} /></details>}
        <details open><summary>messages {request?.incomplete && <strong>{l.partial}</strong>}</summary><Payload data={requestData?.body?.messages ?? request?.data} /></details>
      </> : <p role="status">{l.empty}</p>}
      <h3>{l.responses} <small title={l.limits}>{l.turnLevel}</small></h3>
      {related.length > shown.length && <p className="work-monitor__warning">{l.recent}</p>}
      {shown.map(e => <Entry entry={e} locale={locale} key={e.seq} />)}
    </div>
    {!follow && <button className="work-monitor__follow" type="button" title={l.follow} aria-label={l.follow} onClick={() => setFollow(true)}><ArrowDown size={14} /></button>}
    <PhaseDrum phase={last?.phase} lastActivePhase={lastActivePhase} generation={state.generation} locale={locale} unavailable={!!error} />
  </section>;
}
