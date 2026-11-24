import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { ArrowLeft, ArrowRight, Check, Copy, ExternalLink, Globe, Plus, RotateCw, Search, SquareStop, Trash2, X, ZoomIn, ZoomOut } from "lucide-react";
import { app, onBrowserChanged, openExternal as openInSystemBrowser, setBrowserViewport, type BrowserDownload, type BrowserState, type BrowserUpload } from "../lib/bridge";
import { useT } from "../lib/i18n";

interface Props { ownerTabId: string; visible: boolean; obstructed?: boolean; onTaskStateChange?: (state: string) => void }

const emptyBrowserState: BrowserState = { supported: false, tabs: [], activeTabId: "", busy: false, downloads: [], uploads: [] };

export function normalizeAddress(input: string): string {
  const value = input.trim();
  if (/^(https?:\/\/|about:|file:\/\/)/i.test(value)) return value;
  if (value === "::1") return "http://[::1]";
  const localHost = /^(?:localhost|(?:[a-z0-9-]+\.)+localhost)(?::\d+)?(?:[/?#]|$)/i.test(value);
  const bracketedIPv6 = /^\[[0-9a-f:]+\](?::\d+)?(?:[/?#]|$)/i.test(value);
  const ipv4 = value.match(/^((?:\d{1,3}\.){3}\d{1,3})(?::\d+)?(?:[/?#]|$)/);
  const validIPv4 = Boolean(ipv4 && ipv4[1].split(".").every((octet) => Number(octet) <= 255));
  const localOrIP = localHost || bracketedIPv6 || validIPv4;
  if (localOrIP) return `http://${value}`;
  if (/^(?:[0-9a-f]{0,4}:){2,}[0-9a-f:.]+(?:[/?#].*)?$/i.test(value)) {
    const split = value.search(/[/?#]/);
    const host = split < 0 ? value : value.slice(0, split);
    const suffix = split < 0 ? "" : value.slice(split);
    return `https://[${host}]${suffix}`;
  }
  return `https://${value}`;
}

export function nextZoomFactor(current: number, direction: -1 | 1): number {
  return Math.max(0.5, Math.min(2, Math.round(((current || 1) + direction * 0.1) * 100) / 100));
}

export function browserTaskRunning(taskState?: string): boolean {
  const state = taskState?.trim().toLowerCase();
  return Boolean(state && !["idle", "completed", "complete", "cancelled", "canceled", "failed", "stopped", "done", "success", "succeeded", "error", "timedout", "timeout", "taken_over", "takenover"].includes(state));
}

export function uploadOrigin(upload: BrowserUpload): string {
  if (upload.origin) return upload.origin;
  try { return new URL(upload.url || "about:blank").origin; } catch { return upload.url || ""; }
}

export function normalizeBrowserState(input: BrowserState | null | undefined): BrowserState {
  const state = input && typeof input === "object" ? input : ({} as BrowserState);
  return {
    ...state,
    supported: Boolean(state.supported),
    tabs: Array.isArray(state.tabs) ? state.tabs : [],
    activeTabId: typeof state.activeTabId === "string" ? state.activeTabId : "",
    busy: Boolean(state.busy),
    downloads: Array.isArray(state.downloads) ? state.downloads : [],
    uploads: Array.isArray(state.uploads) ? state.uploads : [],
    taskState: state.taskState ?? "idle",
  };
}

export function BrowserPanel({ ownerTabId, visible, obstructed = false, onTaskStateChange }: Props) {
  const t = useT();
  const [snapshot, setSnapshot] = useState({ owner: ownerTabId, state: emptyBrowserState });
  // Effects from the first render after an owner change must not activate the
  // previous owner's tab before the layout-effect reset has been rendered.
  const state = snapshot.owner === ownerTabId ? snapshot.state : emptyBrowserState;
  const [address, setAddress] = useState("");
  const [findText, setFindText] = useState("");
  const [findOpen, setFindOpen] = useState(false);
  const [error, setError] = useState("");
  const [pendingAction, setPendingAction] = useState<{ kind: "clear" } | { kind: "external"; url: string } | null>(null);
  const [nativeCovered, setNativeCovered] = useState(false);
  const [, setVisibilityRevision] = useState(0);
  const [menuOpen, setMenuOpen] = useState(false);
  const ownerRef = useRef(ownerTabId);
  const mountedRef = useRef(false);
  const activationRetryTimer = useRef<number | null>(null);
  const requestGeneration = useRef(0);
  const stateRequest = useRef<{ owner: string; generation: number; promise: Promise<void> } | null>(null);
  const pollRequest = useRef<{ owner: string; promise: Promise<void> } | null>(null);
  const commandRequest = useRef<{ owner: string; promise: Promise<BrowserState | undefined> } | null>(null);
  const initialActivationRequest = useRef<{ owner: string; promise: Promise<void> } | null>(null);
  const [activationRetry, setActivationRetry] = useState(0);
  const [activatedOwner, setActivatedOwner] = useState("");
  const panelRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<HTMLDivElement>(null);
  const active = state.tabs.find((tab) => tab.id === state.activeTabId);
  const shown = visible && !obstructed && !menuOpen && typeof document !== "undefined" && !document.hidden;
  const nativeVisible = shown && state.supported && activatedOwner === ownerTabId && !state.approval && !state.error && !active?.error && !pendingAction && !nativeCovered;

  useLayoutEffect(() => {
    if (ownerRef.current === ownerTabId) return;
    ownerRef.current = ownerTabId;
    requestGeneration.current++;
    stateRequest.current = null;
    pollRequest.current = null;
    commandRequest.current = null;
    initialActivationRequest.current = null;
    if (activationRetryTimer.current !== null) window.clearTimeout(activationRetryTimer.current);
    activationRetryTimer.current = null;
    setActivatedOwner("");
    setSnapshot({ owner: ownerTabId, state: emptyBrowserState });
    setError("");
    setPendingAction(null);
    setNativeCovered(false);
  }, [ownerTabId]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      requestGeneration.current++;
      if (activationRetryTimer.current !== null) window.clearTimeout(activationRetryTimer.current);
      activationRetryTimer.current = null;
    };
  }, []);

  const refresh = useCallback(async () => {
    if (!ownerTabId) return;
    const activeCommand = commandRequest.current;
    if (activeCommand?.owner === ownerTabId) return activeCommand.promise.then(() => {});
    const pending = stateRequest.current;
    if (pending?.owner === ownerTabId) return pending.promise;
    const generation = ++requestGeneration.current;
    const requestOwner = ownerTabId;
    let promise: Promise<void>;
    promise = (async () => {
      try {
        const next = normalizeBrowserState(await app.GetBrowserState(requestOwner));
        if (ownerRef.current !== requestOwner || requestGeneration.current !== generation) return;
        setSnapshot({ owner: requestOwner, state: next }); setError("");
      } catch (e) {
        if (ownerRef.current === requestOwner && requestGeneration.current === generation) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (stateRequest.current?.generation === generation) stateRequest.current = null;
      }
    })();
    stateRequest.current = { owner: requestOwner, generation, promise };
    return promise;
  }, [ownerTabId]);

  const command = useCallback((op: string, value?: string, extra: { approvalId?: string; allow?: boolean } = {}, targetTabId?: string): Promise<BrowserState | undefined> => {
    if (!ownerTabId) return Promise.resolve(undefined);
    const generation = ++requestGeneration.current;
    const requestOwner = ownerTabId;
    const request = { owner: requestOwner, promise: Promise.resolve(undefined) as Promise<BrowserState | undefined> };
    const promise = (async () => {
      try {
        const next = normalizeBrowserState(await app.BrowserCommand(requestOwner, { op, tabId: targetTabId ?? (state.activeTabId || undefined), value, ...extra }));
        if (ownerRef.current !== requestOwner || requestGeneration.current !== generation) return undefined;
        setSnapshot({ owner: requestOwner, state: next }); setError(next.error ?? "");
        if ((op === "new" || op === "select") && next.supported && next.tabs.length > 0) setActivatedOwner(requestOwner);
        if (op === "navigate") setFindOpen(false);
        return next;
      } catch (e) {
        if (ownerRef.current === requestOwner && requestGeneration.current === generation) setError(e instanceof Error ? e.message : String(e));
        return undefined;
      } finally {
        if (commandRequest.current === request) commandRequest.current = null;
      }
    })();
    request.promise = promise;
    commandRequest.current = request;
    return promise;
  }, [ownerTabId, state.activeTabId]);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    if (!visible || !state.supported || !ownerTabId || activatedOwner === ownerTabId || initialActivationRequest.current?.owner === ownerTabId) return;
    // The task already owns its native tab. Showing it must not issue a manual
    // selection command, which is correctly rejected while that lease is held.
    if (state.busy) {
      if (browserTaskRunning(state.taskState) && state.tabs.some((tab) => tab.id === state.activeTabId)) setActivatedOwner(ownerTabId);
      return;
    }
    const tabId = state.activeTabId || state.tabs[0]?.id;
    const promise = (async () => {
      const next = await command(tabId ? "select" : "new", undefined, {}, tabId);
      if (!mountedRef.current || ownerRef.current !== ownerTabId || activatedOwner === ownerTabId) return;
      if (next?.supported && next.tabs.length > 0) {
        setActivatedOwner(ownerTabId);
        return;
      }
      activationRetryTimer.current = window.setTimeout(() => {
        activationRetryTimer.current = null;
        if (!mountedRef.current || ownerRef.current !== ownerTabId || activatedOwner === ownerTabId) return;
        if (initialActivationRequest.current?.owner === ownerTabId) initialActivationRequest.current = null;
        setActivationRetry((value) => value + 1);
      }, 1000);
    })();
    initialActivationRequest.current = { owner: ownerTabId, promise };
  }, [activationRetry, activatedOwner, command, ownerTabId, state.activeTabId, state.busy, state.supported, state.tabs, state.taskState, visible]);
  useEffect(() => {
    const update = () => setVisibilityRevision((value) => value + 1);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  useEffect(() => {
    const update = () => {
      const surface = viewRef.current?.getBoundingClientRect();
      const open = [...document.querySelectorAll(".modern-chrome__menu[open], [role='menu'], .context-menu")].some((element) => {
        const details = element.closest("details");
        if (details && !details.open) return false;
        if ((element as HTMLElement).hidden || element.getAttribute("aria-hidden") === "true") return false;
        const rect = element.getBoundingClientRect();
        const overlapsSurface = Boolean(surface && rect.right > surface.left && rect.left < surface.right && rect.bottom > surface.top && rect.top < surface.bottom);
        return overlapsSurface && rect.width > 0 && rect.height > 0 && getComputedStyle(element).visibility !== "hidden" && getComputedStyle(element).display !== "none";
      });
      setMenuOpen(open);
    };
    const observer = new MutationObserver(update);
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["open", "class", "role"] });
    const resizeObserver = new ResizeObserver(update);
    if (viewRef.current) resizeObserver.observe(viewRef.current);
    window.addEventListener("resize", update);
    update();
    return () => { observer.disconnect(); resizeObserver.disconnect(); window.removeEventListener("resize", update); };
  }, []);
  useEffect(() => {
    const off = onBrowserChanged(() => { void refresh(); });
    return off;
  }, [refresh]);
  useEffect(() => {
    if (!shown) return;
    const timer = window.setInterval(() => {
      if (pollRequest.current?.owner === ownerTabId) return;
      const promise = refresh().finally(() => { if (pollRequest.current?.promise === promise) pollRequest.current = null; });
      pollRequest.current = { owner: ownerTabId, promise };
    }, 1000);
    return () => window.clearInterval(timer);
  }, [ownerTabId, refresh, shown]);
  useEffect(() => {
    if (!state.supported) return;
    const panel = panelRef.current;
    if (!panel) return;
    const send = () => {
      const rect = viewRef.current?.getBoundingClientRect();
      const visibleNow = Boolean(nativeVisible && rect && rect.width > 0 && rect.height > 0);
      if (!state.supported) return;
      void setBrowserViewport(ownerTabId, {
        x: rect?.left ?? 0, y: rect?.top ?? 0, width: rect?.width ?? 0, height: rect?.height ?? 0,
        scale: window.devicePixelRatio || 1, visible: visibleNow,
      }).catch((e: unknown) => { if (ownerRef.current === ownerTabId) setError(e instanceof Error ? e.message : String(e)); });
    };
    const observer = new ResizeObserver(send);
    observer.observe(panel);
    if (viewRef.current) observer.observe(viewRef.current);
    window.addEventListener("resize", send);
    send();
    return () => { observer.disconnect(); window.removeEventListener("resize", send); void setBrowserViewport(ownerTabId, { x: 0, y: 0, width: 0, height: 0, scale: window.devicePixelRatio || 1, visible: false }).catch(() => {}); };
  }, [ownerTabId, nativeVisible, state.activeTabId, state.supported]);
  useEffect(() => { setAddress(active?.url ?? ""); }, [active?.id, active?.url]);
  useEffect(() => { onTaskStateChange?.(browserTaskRunning(state.taskState) ? state.taskState ?? "" : ""); }, [onTaskStateChange, state.taskState]);

  const navigate = () => {
    const input = address.trim();
    if (!input) return;
    const url = normalizeAddress(input);
    void command("navigate", url);
  };
  const copyAddress = async () => {
    try { await navigator.clipboard.writeText(active?.url ?? address); } catch (e) { setError(e instanceof Error ? e.message : t("browser.copyFailed")); }
  };
  const openExternal = () => {
    const url = active?.url;
    if (url) setPendingAction({ kind: "external", url });
  };
  const confirmAction = () => {
    const action = pendingAction;
    setPendingAction(null);
    if (action?.kind === "clear") void command("clear");
    if (action?.kind === "external") openInSystemBrowser(action.url);
  };
  const zoom = (step: -1 | 1) => {
    const factor = nextZoomFactor(active?.zoom || 1, step);
    void command("zoom", String(factor));
  };
  const formatBytes = (bytes: number) => {
    if (!Number.isFinite(bytes) || bytes < 0) return "0 B";
    if (bytes < 1024) return `${bytes} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let value = bytes / 1024;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
    return `${value.toFixed(value >= 100 ? 0 : value >= 10 ? 1 : 2)} ${units[unit]}`;
  };
  const downloadStatus = (status: string) => {
    const keys = {
      pending: "browser.downloadPending", queued: "browser.downloadPending", downloading: "browser.downloadDownloading",
      completed: "browser.downloadComplete", complete: "browser.downloadComplete", failed: "browser.downloadFailed",
      canceled: "browser.downloadCanceled", cancelled: "browser.downloadCanceled", blocked: "browser.downloadBlocked",
      denied: "browser.downloadDenied", timedout: "browser.downloadTimedOut", interrupted: "browser.downloadFailed",
      inprogress: "browser.downloadDownloading", transferring: "browser.downloadDownloading", finalizing: "browser.downloadFinalizing",
    } as const;
    const key = keys[status.toLowerCase() as keyof typeof keys];
    return key ? t(key) : status;
  };
  const uploadStatus = (upload: BrowserUpload) => {
    const keys: Record<string, "browser.uploadPreparing" | "browser.uploadPending" | "browser.uploadValidating" | "browser.uploadSelected" | "browser.uploadDenied" | "browser.uploadTimedOut" | "browser.uploadStale" | "browser.uploadFailed" | "browser.uploadCancelled" | "browser.uploadUncertain"> = {
      preparing: "browser.uploadPreparing", pending: "browser.uploadPending", validating: "browser.uploadValidating", selected: "browser.uploadSelected",
      denied: "browser.uploadDenied", timedout: "browser.uploadTimedOut", stale: "browser.uploadStale", failed: "browser.uploadFailed",
      cancelled: "browser.uploadCancelled", uncertain: "browser.uploadUncertain",
    };
    const key = keys[upload.status.toLowerCase()];
    return key ? t(key) : upload.status;
  };
  const chooseDownloadDestination = async (download: BrowserDownload) => {
    const requestOwner = ownerTabId;
    setNativeCovered(true);
    try {
      const path = await app.PickExportFile(download.filename || "download", "application/octet-stream");
      if (!path || ownerRef.current !== requestOwner) return;
      void command("download-approve", JSON.stringify({ id: download.id, path }), {}, download.tabId);
    } catch (e) { if (ownerRef.current === requestOwner) setError(e instanceof Error ? e.message : String(e)); }
    finally { if (ownerRef.current === requestOwner) setNativeCovered(false); }
  };
  const chooseUploadFile = async (upload: BrowserUpload) => {
    const requestOwner = ownerTabId;
    setNativeCovered(true);
    try { await command("upload-approve", upload.id, {}, upload.tabId); }
    finally { if (ownerRef.current === requestOwner) setNativeCovered(false); }
  };
  const onPanelKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (!(event.ctrlKey || event.metaKey) || event.altKey) {
      if (event.key === "Escape" && browserTaskRunning(state.taskState)) { event.preventDefault(); event.stopPropagation(); void command("cancelTask"); }
      return;
    }
    const key = event.key.toLowerCase();
    const handled: Record<string, () => void> = {
      l: () => { const input = panelRef.current?.querySelector<HTMLInputElement>(".browser-panel__address input"); input?.focus(); input?.select(); },
      t: () => { void command("new"); },
      w: () => { if (active) void command("close", undefined, {}, active.id); },
      r: () => { void command("reload"); },
      f: () => setFindOpen(true),
      "+": () => zoom(1), "=": () => zoom(1), "-": () => zoom(-1),
    };
    const action = handled[key];
    if (action) { event.preventDefault(); event.stopPropagation(); action(); }
  };

  return <section className="browser-panel" ref={panelRef} aria-label={t("browser.title")} data-native-visible={nativeVisible} onKeyDown={onPanelKeyDown}>
    <div className="browser-panel__tabstrip" role="tablist" aria-label={t("browser.tabs")}>
      {state.tabs.map((tab) => <div className={`browser-panel__tab${tab.id === state.activeTabId ? " browser-panel__tab--active" : ""}`} key={tab.id}>
        <button type="button" role="tab" aria-selected={tab.id === state.activeTabId} title={tab.title || tab.url} onClick={() => void command("select", undefined, {}, tab.id)}>{tab.title || tab.url || t("browser.newTab")}</button>
        <button type="button" className="browser-panel__icon" aria-label={t("browser.closeTab")} title={t("browser.closeTab")} onClick={() => void command("close", undefined, {}, tab.id)}><X size={13} /></button>
      </div>)}
      <button type="button" className="browser-panel__icon" title={t("browser.newTab")} aria-label={t("browser.newTab")} onClick={() => void command("new")}><Plus size={15} /></button>
    </div>
    <div className="browser-panel__toolbar">
      <button type="button" className="browser-panel__icon" disabled={!active?.canGoBack} title={t("browser.back")} aria-label={t("browser.back")} onClick={() => void command("back")}><ArrowLeft size={15} /></button>
      <button type="button" className="browser-panel__icon" disabled={!active?.canGoForward} title={t("browser.forward")} aria-label={t("browser.forward")} onClick={() => void command("forward")}><ArrowRight size={15} /></button>
      <button type="button" className="browser-panel__icon" title={active?.loading ? t("browser.stop") : t("browser.reload")} aria-label={active?.loading ? t("browser.stop") : t("browser.reload")} onClick={() => void command(active?.loading ? "stop" : "reload")}>{active?.loading ? <SquareStop size={14} /> : <RotateCw size={14} />}</button>
      <form className="browser-panel__address" onSubmit={(e) => { e.preventDefault(); navigate(); }}>
        <Globe size={14} aria-hidden="true" />
        <input aria-label={t("browser.address")} value={address} onChange={(e) => setAddress(e.target.value)} placeholder={t("browser.addressPlaceholder")} />
        <button type="button" title={t("browser.copyAddress")} aria-label={t("browser.copyAddress")} onClick={() => void copyAddress()}><Copy size={13} /></button>
      </form>
      <button type="button" className="browser-panel__icon" title={t("browser.find")} aria-label={t("browser.find")} onClick={() => setFindOpen((v) => !v)}><Search size={14} /></button>
      <button type="button" className="browser-panel__icon" title={t("browser.zoomOut")} aria-label={t("browser.zoomOut")} onClick={() => zoom(-1)}><ZoomOut size={14} /></button>
      <button type="button" className="browser-panel__icon" title={t("browser.zoomIn")} aria-label={t("browser.zoomIn")} onClick={() => zoom(1)}><ZoomIn size={14} /></button>
      <button type="button" className="browser-panel__icon" title={t("browser.openExternal")} aria-label={t("browser.openExternal")} onClick={openExternal}><ExternalLink size={14} /></button>
      <button type="button" className="browser-panel__icon" title={t("browser.clearData")} aria-label={t("browser.clearData")} onClick={() => setPendingAction({ kind: "clear" })}><Trash2 size={14} /></button>
    </div>
    {findOpen && <form className="browser-panel__find" onSubmit={(e) => { e.preventDefault(); void command("find", findText); }}><input autoFocus aria-label={t("browser.find")} value={findText} onChange={(e) => setFindText(e.target.value)} placeholder={t("browser.find")} /><button type="submit">{t("browser.findNext")}</button><button type="button" aria-label={t("browser.closeFind")} onClick={() => setFindOpen(false)}><X size={14} /></button></form>}
    {browserTaskRunning(state.taskState) && <div className="browser-panel__task"><span>{t("browser.aiRunning", { state: state.taskState ?? "" })}</span><button type="button" onClick={() => void command("cancelTask")}>{t("browser.stopTask")}</button><button type="button" onClick={() => void command("takeover")}>{t("browser.takeover")}</button></div>}
    {state.approval && <div className="browser-panel__approval" role="alert"><div><strong>{t("browser.approvalTitle", { origin: state.approval.origin })}</strong><p>{state.approval.reason}</p></div><button type="button" onClick={() => void command("approve", undefined, { approvalId: state.approval?.id, allow: true })}><Check size={14} />{t("browser.allow")}</button><button type="button" onClick={() => void command("approve", undefined, { approvalId: state.approval?.id, allow: false })}>{t("browser.deny")}</button></div>}
    {pendingAction && <div className="browser-panel__approval" role="alertdialog" aria-modal="true"><div><strong>{pendingAction.kind === "clear" ? t("browser.clearConfirm") : t("browser.openExternalConfirm", { url: pendingAction.url })}</strong></div><button type="button" onClick={confirmAction}>{t("common.confirm")}</button><button type="button" onClick={() => setPendingAction(null)}>{t("common.cancel")}</button></div>}
    {(error || state.error || active?.error) && <div className="browser-panel__error" role="alert">{error || active?.error || state.error}</div>}
    {state.downloads.length > 0 && <section className="browser-panel__downloads" aria-label={t("browser.downloads")}>
      <h3>{t("browser.downloads")}</h3>
      <ul>{state.downloads.map((download) => {
        const hasTotal = download.totalBytes > 0;
        const progress = hasTotal ? Math.min(100, Math.round(download.bytesReceived / download.totalBytes * 100)) : null;
        const activeDownload = ["pending", "queued", "downloading", "inprogress", "transferring", "finalizing"].includes(download.status.toLowerCase()) || ["inprogress", "transferring", "finalizing"].includes((download.phase || "").toLowerCase());
        return <li key={download.id} className="browser-panel__download">
          <div className="browser-panel__download-heading"><strong title={download.filename}>{download.filename || t("browser.downloadUnnamed")}</strong><span>{downloadStatus(download.phase || download.status)}</span></div>
          <a href="#" onClick={(event) => event.preventDefault()} title={download.url}>{download.url}</a>
          {download.path && <div className="browser-panel__download-path" title={download.path}>{t("browser.downloadPath", { path: download.path })}</div>}
          <div className="browser-panel__download-progress">
            <progress max={hasTotal ? download.totalBytes : undefined} value={download.bytesReceived || undefined} />
            <span>{formatBytes(download.bytesReceived)}{hasTotal ? ` / ${formatBytes(download.totalBytes)} (${progress}%)` : ""}</span>
          </div>
          {download.error && <div className="browser-panel__download-error" role="alert">{download.error}</div>}
          <div className="browser-panel__download-actions">
            {download.requiresApproval && <><button type="button" onClick={() => void chooseDownloadDestination(download)}>{t("browser.downloadApprove")}</button><button type="button" onClick={() => void command("download-deny", download.id, {}, download.tabId)}>{t("browser.downloadDeny")}</button></>}
            {activeDownload && <button type="button" onClick={() => void command("download-cancel", download.id, {}, download.tabId)}>{t("browser.downloadCancel")}</button>}
          </div>
        </li>;
      })}</ul>
    </section>}
    {state.uploads.length > 0 && <section className="browser-panel__uploads" aria-label={t("browser.uploads")}>
      <h3>{t("browser.uploads")}</h3>
      <ul>{state.uploads.map((upload) => {
        const requestPending = ["pending", "preparing"].includes(upload.status.toLowerCase());
        return <li className="browser-panel__upload" key={upload.id}>
          <div className="browser-panel__download-heading"><strong>{uploadOrigin(upload)}</strong><span>{uploadStatus(upload)}</span></div>
          {upload.accept && <div>{t("browser.uploadAccept", { accept: upload.accept })}</div>}
          {upload.multiple && <div>{t("browser.uploadMultiple")}</div>}
          {upload.error && <div className="browser-panel__download-error" role="alert">{upload.error}</div>}
          {requestPending && <div className="browser-panel__download-actions"><button type="button" onClick={() => void chooseUploadFile(upload)}>{t("browser.uploadChoose")}</button><button type="button" onClick={() => void command("upload-deny", upload.id, {}, upload.tabId)}>{t("browser.uploadCancel")}</button></div>}
        </li>;
      })}</ul>
    </section>}
    <div className="browser-panel__viewport" ref={viewRef} aria-label={t("browser.nativeSurface")}>
      {!state.supported && <div className="browser-panel__unsupported"><Globe size={23} /><span>{state.error || t("browser.unsupported")}</span></div>}
    </div>
  </section>;
}
