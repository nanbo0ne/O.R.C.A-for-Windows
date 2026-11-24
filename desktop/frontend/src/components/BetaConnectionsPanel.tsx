import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckCircle2, Circle, Globe, LogIn, LogOut, RefreshCw, Settings, UserRound, Wallet, X } from "lucide-react";
import { useT, type DictKey } from "../lib/i18n";
import { accountErrorKey, betaBridge, readOrcaAccountStatus, exactCnyAmount, hideWebBeta, receiptCost, receiptWarningKey, retainedSettledSubtotal, retainedTurnCost, setWebBetaBounds, webErrorKey, type BetaCapabilities, type OrcaAccountReceipt, type OrcaAccountStatus, type OrcaAccountWallet, type OrcaTurnSettlement, type WebBetaSettings, type WebBetaStatus } from "../lib/betaBridge";
import "./beta-connections.css";

const accountLabels: Record<OrcaAccountStatus["state"], DictKey> = {
  signed_out: "beta.signedOut", signing_in: "beta.authorizing", signed_in: "beta.signedIn",
  restoring: "beta.loading", logging_out: "beta.loggingOut", error: "beta.unavailable",
};

const receiptLabels: Record<string, DictKey> = { unknown: "beta.moneyUnknown", pending: "beta.pending", settled: "beta.settled", estimated: "beta.estimated" };

function AccountDetailsDialog({ onClose, receiptWarning }: { onClose(): void; receiptWarning: DictKey | null }) {
  const t = useT();
  const [wallet, setWallet] = useState<OrcaAccountWallet | null>(null);
  const [receipts, setReceipts] = useState<OrcaAccountReceipt[]>([]);
  const [turns, setTurns] = useState<OrcaTurnSettlement[] | null>(null);
  const [errors, setErrors] = useState<{ receipts?: DictKey; wallet?: DictKey; turns?: DictKey }>({});
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const refreshRef = useRef<(force?: boolean) => void>(() => {});
  useEffect(() => {
    let disposed = false;
    let pending = false;
    let lastWallet = 0;
    let lastSettlements = "";
    const refresh = async (force = false) => {
      if (pending || disposed) return;
      pending = true; setBusy(true);
      let settlements = lastSettlements;
      try {
        const next = await betaBridge().OrcaAccountReceipts();
        if (disposed) return;
        const rows = Array.isArray(next) ? next : [];
        settlements = JSON.stringify(rows.filter(row => row.status === "settled").map(row => [row.requestId, row.cost, row.currency]).sort((a, b) => a[0].localeCompare(b[0])));
        setReceipts(rows); setLoaded(true);
        setErrors(value => ({ ...value, receipts: undefined }));
      } catch (failure) { if (!disposed) { setReceipts([]); setLoaded(true); setErrors(value => ({ ...value, receipts: accountErrorKey(failure) })); } }
      try {
        if (disposed) return;
        const next = await betaBridge().OrcaAccountTurnSettlements();
        if (disposed) return;
        setTurns(Array.isArray(next) ? next.filter(row => row.scope === "retained_requests") : []);
        setErrors(value => ({ ...value, turns: undefined }));
      } catch (failure) { if (!disposed) { setTurns([]); setErrors(value => ({ ...value, turns: accountErrorKey(failure) })); } }
      try {
        if (disposed) return;
        if (force || !lastWallet || Date.now() - lastWallet >= 10000 || settlements !== lastSettlements) {
          lastWallet = Date.now(); lastSettlements = settlements;
          const balance = await betaBridge().OrcaAccountWallet();
          if (disposed) return;
          setWallet(balance);
          setErrors(value => ({ ...value, wallet: balance.error ? accountErrorKey(balance.error) : undefined }));
        }
      } catch (failure) { if (!disposed) setErrors(value => ({ ...value, wallet: accountErrorKey(failure) })); }
      finally { pending = false; if (!disposed) setBusy(false); }
    };
    refreshRef.current = force => { void refresh(force); };
    // Deferral also prevents duplicate initial reads during StrictMode probes.
    const start = window.setTimeout(() => void refresh(), 0);
    const timer = window.setInterval(() => void refresh(), 2000);
    return () => { disposed = true; window.clearTimeout(start); window.clearInterval(timer); };
  }, []);
  const money = (value: unknown, currency: unknown) => {
    const amount = exactCnyAmount(value, currency);
    return amount === null ? t("beta.moneyUnknown") : `CNY ${amount}`;
  };
  return <BetaDialog title={t("beta.accountDetails")} onClose={onClose}>
    <div className="beta-account-details">
      <div className="beta-account-section-title"><h3>{t("beta.balance")}</h3><button type="button" className="beta-connections-icon" aria-label={t("beta.refreshAccount")} title={t("beta.refreshAccount")} disabled={busy} onClick={() => refreshRef.current(true)}><RefreshCw size={15} /></button></div>
      <dl className="beta-account-balances">
        {([["cash", "beta.cash"], ["held", "beta.held"], ["available", "beta.available"]] as const).map(([key, label]) => <div key={key}><dt>{t(label)}</dt><dd data-money={key}>{money(errors.wallet || wallet?.error ? undefined : wallet?.[key], wallet?.currency)}</dd></div>)}
      </dl>
      <h3>{t("beta.scopedGifts")}</h3>
      {errors.wallet || wallet?.error ? <p>{t("beta.moneyUnknown")}</p> : !wallet ? <p role="status">{t("beta.loading")}</p> : !wallet.gifts?.length ? <p>{t("beta.noGifts")}</p> : <ul className="beta-account-gifts">{wallet.gifts.map((gift, index) => <li key={index}><span>{gift.groups ? t("beta.giftGroups", { groups: gift.groups }) : t("beta.unknownScope")}</span><strong>{money(gift.available, wallet.currency)}</strong></li>)}</ul>}
      <h3>{t("beta.receipts")}</h3>
      {receiptWarning && <p className="beta-connections-error" data-receipt-warning role="alert">{t(receiptWarning)}</p>}
      {errors.receipts ? <p>{t("beta.moneyUnknown")}</p> : !loaded ? <p role="status">{t("beta.loading")}</p> : receipts.length === 0 ? <p>{t("beta.noReceipts")}</p> : <div className="beta-account-receipts" tabIndex={0}><table><thead><tr><th>{t("beta.request")}</th><th>{t("beta.receiptStatus")}</th><th>{t("beta.cost")}</th></tr></thead><tbody>{receipts.map(row => <tr key={row.requestId} data-receipt-id={row.requestId}><td><strong>{row.purpose || t("beta.request")}</strong><span>{row.requestId}</span>{row.turnId && <span>{t("beta.turn", { id: row.turnId })}</span>}</td><td>{t(receiptLabels[row.status] || "beta.moneyUnknown")}</td><td>{money(receiptCost(row), row.currency)}</td></tr>)}</tbody></table></div>}
      <details className="beta-account-turns">
        <summary>{t("beta.retainedTurns")}</summary>
        {errors.turns ? <p>{t("beta.moneyUnknown")}</p> : !turns ? <p role="status">{t("beta.loading")}</p> : turns.length === 0 ? <p>{t("beta.noRetainedTurns")}</p> : <div className="beta-account-receipts beta-account-turn-table" tabIndex={0}><table><thead><tr><th>{t("beta.turnId")}</th><th>{t("beta.receiptStatus")}</th><th>{t("beta.cost")}</th><th>{t("beta.settledSubtotal")}</th></tr></thead><tbody>{turns.map(row => {
          const cost = retainedTurnCost(row), subtotal = retainedSettledSubtotal(row);
          return <tr key={row.turnId} data-turn-id={row.turnId}><td><strong>{row.turnId}</strong><span>{t("beta.settledRequestCount", { settled: row.settledRequests, requests: row.requests })}</span></td><td>{t(receiptLabels[row.status] || "beta.moneyUnknown")}</td><td data-turn-cost aria-label={cost === null ? t("beta.moneyUnknown") : undefined}>{cost === null ? "" : `CNY ${cost}`}</td><td data-turn-subtotal aria-label={subtotal === null ? t("beta.moneyUnknown") : undefined}>{subtotal === null ? "" : `CNY ${subtotal}`}</td></tr>;
        })}</tbody></table></div>}
      </details>
      {[...new Set(Object.values(errors))].filter((key): key is DictKey => Boolean(key)).map(key => <p key={key} className="beta-connections-error" role="alert">{t(key)}</p>)}
    </div>
  </BetaDialog>;
}

function BetaDialog({ title, children, onClose, wide = false, closeDisabled = false }: { title: string; children: ReactNode; onClose(): void; wide?: boolean; closeDisabled?: boolean }) {
  const t = useT();
  const titleId = useId();
  const backdrop = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const focusRevision = useRef(0);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  useLayoutEffect(() => {
    if (!returnFocus.current) returnFocus.current = document.activeElement as HTMLElement | null;
    const previous = returnFocus.current;
    const revision = ++focusRevision.current;
    const siblings = Array.from(document.body.children).filter((node): node is HTMLElement => node instanceof HTMLElement && node !== backdrop.current);
    const prior = siblings.map(node => node.inert);
    siblings.forEach(node => { node.inert = true; });
    close.current?.focus();
    const active = () => panel.current && !panel.current.closest("[inert]");
    const focusables = () => Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),summary,[tabindex="0"]') || []).filter(node => node.getClientRects().length > 0);
    const keys = (event: KeyboardEvent) => {
      if (!active()) return;
      if (event.key === "Escape") {
        event.preventDefault(); event.stopImmediatePropagation(); onCloseRef.current();
      } else if (event.key === "Tab") {
        const nodes = focusables();
        const index = nodes.indexOf(document.activeElement as HTMLElement);
        if (index < 0 || (event.shiftKey && index === 0) || (!event.shiftKey && index === nodes.length - 1)) {
          event.preventDefault(); (event.shiftKey ? nodes[nodes.length - 1] : nodes[0])?.focus();
        }
        event.stopPropagation();
      } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault(); event.stopImmediatePropagation();
      }
    };
    const focus = (event: FocusEvent) => {
      if (active() && !panel.current?.contains(event.target as Node)) close.current?.focus();
    };
    window.addEventListener("keydown", keys, true);
    document.addEventListener("focusin", focus);
    return () => {
      window.removeEventListener("keydown", keys, true);
      document.removeEventListener("focusin", focus);
      siblings.forEach((node, i) => { node.inert = prior[i]; });
      requestAnimationFrame(() => {
        if (focusRevision.current !== revision) return;
        const target = previous?.closest("details")?.querySelector("summary") ?? previous;
        if (target?.isConnected && target !== document.body && target !== document.documentElement && target.getClientRects().length > 0 && !target.closest("[inert]")) target.focus();
        else Array.from(document.querySelectorAll<HTMLElement>('[data-beta-entry], .modern-chrome__menu summary, .app-chrome__command, .topicbar__overflow button')).find(node => node.getClientRects().length > 0 && !node.closest("[inert]"))?.focus();
      });
    };
  }, []);
  return createPortal(<div ref={backdrop} className="beta-connections-backdrop">
    <section ref={panel} className={`beta-connections-dialog${wide ? " beta-connections-dialog--web" : ""}`} role="dialog" aria-modal="true" aria-labelledby={titleId}>
      <header className="beta-connections-header"><h2 id={titleId}>{title}</h2><button ref={close} type="button" disabled={closeDisabled} className="beta-connections-icon" aria-label={t("common.close")} title={t("common.close")} onClick={onClose}><X size={18} /></button></header>
      {children}
    </section>
  </div>, document.body);
}

let presentationGeneration = 0;
function DeepSeekLoginDialog({ onClose, obstructed }: { onClose(): void; obstructed: boolean }) {
  const t = useT();
  const view = useRef<HTMLDivElement>(null);
  const [opened, setOpened] = useState(false);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let disposed = false;
    const generation = ++presentationGeneration;
    setOpened(false); setFailed(false);
    const timer = window.setTimeout(() => void Promise.resolve().then(() => betaBridge().WebBetaOpen()).then(() => {
      if (!disposed) setOpened(true);
      else if (generation === presentationGeneration) void hideWebBeta().catch(() => {});
    }).catch(() => { if (!disposed) setFailed(true); }), 0);
    return () => { disposed = true; window.clearTimeout(timer); void hideWebBeta().catch(() => {}); };
  }, [attempt]);
  useEffect(() => {
    if (!opened || !view.current) return;
    let disposed = false;
    const sync = () => {
      if (disposed || !view.current) return;
      const r = view.current.getBoundingClientRect();
      const d = window.devicePixelRatio || 1;
      const visible = !obstructed && !document.hidden && r.width > 0 && r.height > 0;
      void setWebBetaBounds({ x: Math.round(r.x * d), y: Math.round(r.y * d), width: Math.round(r.width * d), height: Math.round(r.height * d), visible }).catch(() => { if (!disposed) setFailed(true); });
    };
    const observer = new ResizeObserver(sync);
    observer.observe(view.current);
    window.addEventListener("resize", sync);
    document.addEventListener("visibilitychange", sync);
    sync();
    return () => { disposed = true; observer.disconnect(); window.removeEventListener("resize", sync); document.removeEventListener("visibilitychange", sync); void hideWebBeta().catch(() => {}); };
  }, [opened, obstructed]);
  return <BetaDialog title={t("beta.webLogin")} wide onClose={onClose}>
    <div className="beta-connections-webview" ref={view} data-testid="beta-webview">
      {!opened && <div role={failed ? "alert" : "status"}>{t(failed ? "beta.requestFailed" : "beta.openingLogin")}{failed && <button type="button" onClick={() => setAttempt(value => value + 1)}><RefreshCw size={15} />{t("beta.retry")}</button>}</div>}
    </div>
    <footer className="beta-connections-footer" role={failed ? "alert" : "status"}>{t(failed ? "beta.requestFailed" : obstructed ? "beta.webHidden" : "beta.webLoginStatus")}</footer>
  </BetaDialog>;
}

function DeepSeekSettingsDialog({ status, onClose, onRefresh, onRecovered, onOpenWeb }: { status: WebBetaStatus; onClose(): void; onRefresh(): void; onRecovered(status: WebBetaStatus): void; onOpenWeb(): void }) {
  const t = useT();
  const [draft, setDraft] = useState<WebBetaSettings>(() => ({ ...status.settings, enabled: status.sendAuthorized, openings: [...status.settings.openings] }));
  const [consent, setConsent] = useState(false);
  const [reserveConsent, setReserveConsent] = useState(false);
  const [recovered, setRecovered] = useState(false);
  const [switched, setSwitched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<DictKey | null>(null);
  const pending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const run = async (action: () => Promise<void>, recovery = false, accountSwitch = false) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true); setFailure(null);
    try {
      await action();
      const recoveredStatus = recovery ? await betaBridge().WebBetaStatus() : null;
      if (mounted.current) {
        if (recoveredStatus) {
          // Use the saved backend grant, never an obsolete draft or inference.
          setDraft({ ...recoveredStatus.settings, openings: [...recoveredStatus.settings.openings] });
          setConsent(false); setReserveConsent(false); setRecovered(true);
          setSwitched(accountSwitch);
          onRecovered(recoveredStatus);
        }
        onRefresh();
        if (!recovery) onClose();
      }
    }
    catch (error) { if (mounted.current) setFailure(webErrorKey(error)); }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
  };
  const needsConsent = draft.enabled && !status.sendAuthorized;
  const needsReserveConsent = draft.enabled && !draft.reservePaused && (recovered || !status.settings.enabled || status.settings.reservePaused);
  const blocked = Boolean(status.blocked || status.browser.blocked);
  const identityChanged = failure === "beta.webError.identity" || status.browser.blocked === "identity_changed" || status.browser.blocked === "identity_storage_failed" || status.blocked === "identity_switch_pending";
  const canSave = !busy && (!draft.enabled || (status.browser.ready && !blocked)) && (!needsConsent || consent) && (!needsReserveConsent || reserveConsent);
  return <BetaDialog title={t("beta.webSettings")} closeDisabled={busy} onClose={() => { if (!pending.current) onClose(); }}>
    <form className="beta-connections-settings" onSubmit={event => { event.preventDefault(); if (canSave) void run(() => betaBridge().WebBetaConfigure(draft)); }}>
      <label><input type="checkbox" disabled={busy} checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} />{t("beta.enableWeb")}</label>
      <label className="beta-connections-slider">{t("beta.rpm")}<output>{draft.rpm}</output><input type="range" disabled={busy} min="1" max="10" step="1" value={draft.rpm} onChange={e => setDraft({ ...draft, rpm: Number(e.target.value) })} /></label>
      <label><input type="checkbox" disabled={busy} checked={draft.thinking} onChange={e => setDraft({ ...draft, thinking: e.target.checked })} />{t("beta.thinking")}</label>
      <label><input type="checkbox" disabled={busy} checked={draft.search} onChange={e => setDraft({ ...draft, search: e.target.checked })} />{t("beta.search")}</label>
      <label><input type="checkbox" disabled={busy} checked={draft.reservePaused} onChange={e => setDraft({ ...draft, reservePaused: e.target.checked })} />{t("beta.pauseReserve")}</label>
      {!draft.reservePaused && <fieldset><legend>{t("beta.openings")}</legend>
        <label>{t("beta.openingRounds")}<select disabled={busy} value={draft.openingRounds} onChange={e => setDraft({ ...draft, openingRounds: Number(e.target.value) })}><option value="1">1</option><option value="2">2</option></select></label>
        {Array.from({ length: Math.min(2, Math.max(1, draft.openingRounds)) }, (_, index) => <label key={index}>{t("beta.opening", { n: index + 1 })}<textarea disabled={busy} maxLength={500} value={draft.openings[index] ?? ""} onChange={e => { const openings = [...draft.openings]; openings[index] = e.target.value; setDraft({ ...draft, openings }); }} /></label>)}
      </fieldset>}
      {needsConsent && <label className="beta-connections-consent"><input type="checkbox" disabled={busy} checked={consent} onChange={e => setConsent(e.target.checked)} />{t("beta.consent")}</label>}
      {needsReserveConsent && <label className="beta-connections-consent"><input type="checkbox" disabled={busy} checked={reserveConsent} onChange={e => setReserveConsent(e.target.checked)} />{t("beta.reserveConsent")}</label>}
      {recovered && <p role="status" data-web-recovered>{t(switched ? "beta.identitySwitched" : status.sendAuthorized ? "beta.recoveredAuthorized" : "beta.recovered")}</p>}
      {identityChanged && <p role="status">{t("beta.identitySwitchNotice")}</p>}
      {failure && <p role="alert">{t(failure)}</p>}
      <div className="beta-connections-actions">
        {(identityChanged || status.browser.ready) && <button type="button" disabled={busy} title={t("beta.identitySwitchNotice")} onClick={() => void run(() => betaBridge().WebBetaUseCurrentLogin(), true, true)}><UserRound size={15} />{t("beta.useCurrentLogin")}</button>}
        {!identityChanged && (blocked || status.error) && <button type="button" disabled={busy} onClick={() => void run(() => betaBridge().WebBetaRecover(), true)}><RefreshCw size={15} />{t("beta.recover")}</button>}
        <button type="button" disabled={busy} onClick={() => void run(() => betaBridge().WebBetaLogout())}><LogOut size={15} />{t("beta.webLogout")}</button>
        <button type="button" disabled={busy} onClick={onOpenWeb}><Globe size={15} />{t("beta.openWeb")}</button>
        <button type="submit" disabled={!canSave}>{t("common.save")}</button>
      </div>
    </form>
  </BetaDialog>;
}

export function BetaConnectionsPanel({ capabilities, onClose, onAccountChanged, nativeObstructed = false }: { capabilities: BetaCapabilities; onClose(): void; onAccountChanged?(): void; nativeObstructed?: boolean }) {
  const t = useT();
  const [account, setAccount] = useState<OrcaAccountStatus | null>(null);
  const [web, setWeb] = useState<WebBetaStatus | null>(null);
  const [accountError, setAccountError] = useState<DictKey | null>(null);
  const [webFailed, setWebFailed] = useState(false);
  const [accountBusy, setAccountBusy] = useState(false);
  const [nested, setNested] = useState<"login" | "settings" | "account" | null>(null);
  const [webReturn, setWebReturn] = useState<"settings" | null>(null);
  const [revision, setRevision] = useState(0);
  const mounted = useRef(true);
  const polling = useRef(false);
  const webStatusEpoch = useRef(0);
  const lastAuthenticated = useRef<boolean | null>(null);
  const accountChanged = useRef(onAccountChanged);
  accountChanged.current = onAccountChanged;
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    let disposed = false;
    const refresh = async () => {
      if (polling.current) return;
      polling.current = true;
      const webEpoch = webStatusEpoch.current;
      await Promise.all([
        capabilities.account ? readOrcaAccountStatus().then(value => {
          if (disposed) return;
          setAccount(value); setAccountError(value.error ? accountErrorKey(value.error) : null);
          const previous = lastAuthenticated.current;
          lastAuthenticated.current = value.authenticated;
          if (value.authenticated !== previous && (value.authenticated || previous === true)) accountChanged.current?.();
        }).catch(error => { if (!disposed) setAccountError(accountErrorKey(error)); }) : Promise.resolve(),
        capabilities.web ? Promise.resolve().then(() => betaBridge().WebBetaStatus()).then(value => { if (!disposed && webEpoch === webStatusEpoch.current) { setWeb(value); setWebFailed(false); } }).catch(() => { if (!disposed && webEpoch === webStatusEpoch.current) setWebFailed(true); }) : Promise.resolve(),
      ]);
      polling.current = false;
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 1500);
    return () => { disposed = true; window.clearInterval(timer); };
  }, [capabilities.account, capabilities.web, revision]);
  const accountLogout = Boolean(account?.authenticated || account?.needsSignOut);
  const accountAction = async () => {
    if (accountBusy || account?.state === "logging_out" || account?.state === "restoring") return;
    setAccountBusy(true); setAccountError(null);
    try {
      if (account?.state === "signing_in") await betaBridge().OrcaAccountCancelLogin();
      else if (accountLogout) await betaBridge().OrcaAccountLogout();
      else await betaBridge().OrcaAccountLogin();
      if (mounted.current) setRevision(value => value + 1);
    } catch (error) { if (mounted.current) setAccountError(accountErrorKey(error)); }
    finally { if (mounted.current) setAccountBusy(false); }
  };
  const accountCancel = account?.state === "signing_in";
  const receiptWarning = receiptWarningKey(account?.receiptError);
  const connected = Boolean(web?.browser.ready || web?.browser.busy);
  const webLabel: DictKey = webFailed ? "beta.unavailable" : !web ? "beta.loading" : web.error || web.blocked || web.browser.blocked ? "beta.attention" : connected ? web.settings.enabled ? "beta.connected" : "beta.loginRetained" : "beta.notConnected";
  return <>
    <BetaDialog title={t("beta.connections")} onClose={onClose}>
      <div className="beta-connections-list">
        {capabilities.account && <section className="beta-connections-row" aria-label={t("beta.account")}>
          <UserRound size={19} /><div className="beta-connections-details"><h3>{t("beta.account")}</h3><p role="status">{t(account ? accountLabels[account.state] || "beta.unavailable" : accountError ? "beta.unavailable" : "beta.loading")}</p>{account?.displayName && <p>{account.displayName}</p>}</div>
          <div className="beta-connections-actions"><button type="button" disabled={!account || accountBusy || account.state === "restoring" || account.state === "logging_out"} onClick={() => void accountAction()}>{accountCancel ? <X size={15} /> : accountLogout ? <LogOut size={15} /> : <LogIn size={15} />}{t(accountCancel ? "beta.cancelLogin" : accountLogout ? "beta.logout" : "beta.accountSignIn")}</button>
          {account?.authenticated && account.state === "signed_in" && <button type="button" className="beta-connections-icon" aria-label={t("beta.accountDetails")} title={t("beta.accountDetails")} onClick={() => setNested("account")}><Wallet size={16} /></button>}</div>
          {accountError && <p className="beta-connections-error" role="alert">{t(accountError)}</p>}
          {receiptWarning && <p className="beta-connections-error" data-receipt-warning role="alert">{t(receiptWarning)}</p>}
        </section>}
        {capabilities.web && <section className="beta-connections-row" aria-label={t("beta.web")}>
          <Globe size={19} /><div className="beta-connections-details"><h3>{t("beta.web")}</h3><p role="status">{connected ? <CheckCircle2 size={13} /> : <Circle size={13} />}{t(webLabel)}</p></div>
          <div className="beta-connections-actions"><button type="button" onClick={() => setNested("login")}><LogIn size={15} />{t("beta.login")}</button><button type="button" className="beta-connections-icon" disabled={!web} aria-label={t("beta.webSettings")} title={t("beta.webSettings")} onClick={() => setNested("settings")}><Settings size={16} /></button></div>
          {webFailed && <p className="beta-connections-error" role="alert">{t("beta.requestFailed")}</p>}
        </section>}
      </div>
    </BetaDialog>
    {(nested === "settings" || (nested === "login" && webReturn === "settings")) && web && <DeepSeekSettingsDialog status={web} onOpenWeb={() => { setWebReturn("settings"); setNested("login"); }} onRefresh={() => setRevision(value => value + 1)} onClose={() => setNested(null)} onRecovered={status => {
      // A late pre-recovery poll must not overwrite the authoritative result.
      webStatusEpoch.current++;
      setWeb(status);
      setWebFailed(false);
    }} />}
    {nested === "login" && <DeepSeekLoginDialog obstructed={nativeObstructed} onClose={() => { setNested(webReturn); setWebReturn(null); setRevision(value => value + 1); }} />}
    {nested === "account" && account?.authenticated && account.state === "signed_in" && <AccountDetailsDialog receiptWarning={receiptWarning} onClose={() => setNested(null)} />}
  </>;
}
