import { useCallback, useEffect, useRef, useState } from "react";
import { ExternalLink, Loader2, RefreshCw, UserRound } from "lucide-react";
import { app, type WhaleStackDeviceView, type WhaleStackStatusView } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { syncWhaleCatalog } from "../lib/whaleCatalogSync";

type SignInPhase = { kind: "idle" } | { kind: "waiting"; device: WhaleStackDeviceView } | { kind: "failed"; message: string };

// The capsule answers "who am I signed in as" without a trip through settings.
// Signing in happens in the real browser: the app only carries a one-time code,
// so the password is typed on the login origin and never reaches this process.
export function AccountCapsule() {
  const t = useT();
  const [status, setStatus] = useState<WhaleStackStatusView>({ signedIn: false });
  const [open, setOpen] = useState(false);
  const [phase, setPhase] = useState<SignInPhase>({ kind: "idle" });
  const [refreshing, setRefreshing] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const cancelledRef = useRef(false);

  const refresh = useCallback(() => {
    void app.WhaleStackStatus().then((next) => setStatus(next ?? { signedIn: false })).catch(() => undefined);
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("mousedown", onPointer);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onPointer);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  useEffect(() => {
    const onSignedIn = () => refresh();
    const onModels = () => refresh();
    window.addEventListener("orca:whalestack-signed-in", onSignedIn);
    window.addEventListener("orca:whalestack-models", onModels);
    return () => {
      window.removeEventListener("orca:whalestack-signed-in", onSignedIn);
      window.removeEventListener("orca:whalestack-models", onModels);
    };
  }, [refresh]);

  // The relay decides which models the account can reach, and the serving model
  // can change underneath us; a pull here updates every picker at once because
  // they all read the same provider entry.
  const refreshModels = useCallback(async () => {
    setRefreshing(true);
    try {
      await syncWhaleCatalog();
      refresh();
    } catch {
      // Keep the last known list: a flaky relay must not empty the picker.
    } finally {
      setRefreshing(false);
    }
  }, [refresh]);

  useEffect(() => {
    if (!status.signedIn) return;
    const update = () => { if (!document.hidden) void syncWhaleCatalog().catch(() => {}); };
    const timer = window.setInterval(update, 10000);
    window.addEventListener("focus", update); document.addEventListener("visibilitychange", update);
    if (open) update();
    return () => { clearInterval(timer);window.removeEventListener("focus",update);document.removeEventListener("visibilitychange",update); };
  }, [status.signedIn, open]);

  const startSignIn = useCallback(async () => {
    cancelledRef.current = false;
    setOpen(true);
    try {
      const device = await app.WhaleStackStartSignIn();
      setPhase({ kind: "waiting", device });
      const view = await app.WhaleStackFinishSignIn(device.device);
      if (cancelledRef.current) return;
      setPhase({ kind: "idle" });
      setOpen(false);
      refresh();
      void view;
      window.dispatchEvent(new CustomEvent("orca:whalestack-signed-in"));
    } catch (err) {
      if (cancelledRef.current) return;
      setPhase({ kind: "failed", message: err instanceof Error ? err.message : String(err) });
    }
  }, [refresh]);

  const label = status.signedIn
    ? status.name || status.email || t("account.signedIn")
    : phase.kind === "waiting"
      ? t("account.waiting")
      : t("account.signIn");

  return (
    <div className="account-capsule-wrap" ref={rootRef}>
      <button
        type="button"
        className={`account-capsule${status.signedIn ? " is-signed-in" : ""}${phase.kind === "waiting" ? " is-waiting" : ""}`}
        onClick={() => (status.signedIn ? setOpen((v) => !v) : void startSignIn())}
        title={status.signedIn ? `${t("account.signedIn")}：${status.email || status.name || ""}` : t("account.signInHint")}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        {phase.kind === "waiting" ? <Loader2 size={13} className="account-capsule__spin" /> : <UserRound size={13} strokeWidth={2.1} />}
        <span className="account-capsule__label">{label}</span>
        {status.signedIn && <span className="account-capsule__dot" aria-hidden="true" />}
      </button>
      {open && (
        <div className="account-popover" role="dialog" aria-label={t("account.signInTitle")}>
          {status.signedIn ? (
            <>
              <div className="account-popover__head">
                <span className="account-popover__avatar"><UserRound size={16} /></span>
                <div>
                  <div className="account-popover__name">{status.name || status.email}</div>
                  {status.name && status.email && <div className="account-popover__email">{status.email}</div>}
                </div>
              </div>
              {status.models && status.models.length > 0 && (
                <div className="account-popover__models">
                  <div className="account-popover__caption">{t("account.models")}</div>
                  <div className="account-popover__chips">
                    {status.models.slice(0, 8).map((model) => <span key={model} className="account-popover__chip">{model}</span>)}
                  </div>
                </div>
              )}
              <button
                type="button"
                className="account-popover__secondary"
                onClick={() => void refreshModels()}
                disabled={refreshing}
              >
                <RefreshCw size={12} className={refreshing ? "account-capsule__spin" : undefined} />
                {t("account.refreshModels")}
              </button>
              <button
                type="button"
                className="account-popover__secondary"
                onClick={() => {
                  setOpen(false);
                  window.dispatchEvent(new CustomEvent("orca:open-settings", { detail: "providers" }));
                }}
              >
                {t("account.openSettings")}
              </button>
            </>
          ) : phase.kind === "waiting" ? (
            <div className="account-popover__body">
              <div className="account-popover__caption">{t("account.waitingTitle")}</div>
              <div className="account-code">{phase.device.code}</div>
              <p className="account-popover__hint">{t("account.waitingHint")}</p>
              <div className="account-popover__row">
                <a className="account-popover__link" href={phase.device.verifyUrl} target="_blank" rel="noreferrer">
                  <ExternalLink size={12} /> {t("account.reopen")}
                </a>
                <button type="button" className="account-popover__text" onClick={() => { cancelledRef.current = true; setPhase({ kind: "idle" }); setOpen(false); }}>
                  {t("account.cancel")}
                </button>
              </div>
            </div>
          ) : (
            <div className="account-popover__body">
              <div className="account-popover__caption">{t("account.browserTitle")}</div>
              <p className="account-popover__hint">{t("account.browserHint")}</p>
              {phase.kind === "failed" && <div className="account-popover__error">{phase.message}</div>}
              <button type="button" className="account-popover__primary" onClick={() => void startSignIn()}>
                {t("account.openBrowser")}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
