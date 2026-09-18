import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { Check, ChevronsUpDown, Gauge } from "lucide-react";
import { asArray } from "../lib/array";
import { useT } from "../lib/i18n";
import type { EffortInfo } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

export function EffortSwitcher({
  effort,
  disabled,
  onPick,
  onConfigure,
  showDefault = false,
  saving = false,
  running = false,
}: {
  effort?: EffortInfo;
  disabled: boolean;
  onPick: (level: string) => Promise<void> | void;
  onConfigure?: () => void;
  showDefault?: boolean;
  saving?: boolean;
  running?: boolean;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const levels = asArray(effort?.levels);
  const current = effort?.current || "auto";
  const supported = Boolean(effort?.supported && levels.length > 0);
  const locked = disabled || saving || running;
  const hint = saving ? t("status.effortSaving") : running ? t("status.effortRunningHint") : undefined;
  const autoTitle = effort?.default && effort.default !== "auto"
    ? t("status.effortAutoTitle", { def: effort.default })
    : t("status.effortAutoOmittedTitle");
  const title = !supported ? t("status.effortModelDefaultHint") : current === "auto" ? autoTitle : t("status.effortTitle");

  const clearCloseTimer = useCallback(() => {
    if (closeTimerRef.current === null) return;
    window.clearTimeout(closeTimerRef.current);
    closeTimerRef.current = null;
  }, []);

  const openMenu = useCallback(() => {
    if (locked) return;
    clearCloseTimer();
    setClosing(false);
    setOpen(true);
  }, [clearCloseTimer, locked]);

  const closeMenu = useCallback((afterClose?: () => void) => {
    if (menuRef.current?.contains(document.activeElement)) triggerRef.current?.focus();
    clearCloseTimer();
    setClosing(true);
    window.requestAnimationFrame(() => setOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      setClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearCloseTimer]);

  useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

  useEffect(() => {
    if (!locked) return;
    clearCloseTimer();
    setOpen(false);
    setClosing(false);
  }, [locked, clearCloseTimer]);

  useEffect(() => {
    if (!open || closing) return;
    const frame = requestAnimationFrame(() => {
      if (document.activeElement === triggerRef.current) {
        (menuRef.current?.querySelector<HTMLButtonElement>('[aria-selected="true"]') || menuRef.current?.querySelector<HTMLButtonElement>("button"))?.focus();
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [open, closing]);

  const navigate = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"));
    if (!buttons.length) return;
    event.preventDefault();
    const currentIndex = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (currentIndex + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
    buttons[next].focus();
  };

  const pick = (level: string) => {
    if (locked || closing) return;
    closeMenu();
    // The parent owns pending state and reports save failures.
    if (level !== current) return onPick(level);
  };

  if (!supported && !showDefault) return null;

  return (
    <div className="modelsw effortsw" title={hint ? `${hint} ${title}` : title}>
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger effortsw__trigger ${current !== "auto" ? "effortsw__trigger--explicit" : ""}`}
        disabled={locked}
        title={hint ? `${hint} ${title}` : title}
        aria-busy={saving}
        aria-description={hint}
        aria-label={`${t("status.effortTitle")}: ${supported ? current : t("status.effortModelDefault")}`}
        aria-haspopup={supported ? "listbox" : "dialog"}
        aria-expanded={open && !closing && !locked}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); openMenu(); }
        }}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <Gauge size={13} className="modelsw__kind" />
        <span className="modelsw__label">{saving ? t("status.effortSaving") : supported ? current : t("status.effortModelDefault")}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !locked}
        closing={closing && !locked}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="modelsw__menu modelsw__menu--portal effortsw__menu"
        align="end"
      >
        {supported ? <div ref={menuRef} onKeyDown={navigate} role="listbox" aria-label={t("status.effortTitle")}>
          {levels.map((level) => (
            <button
              key={level}
              type="button"
              role="option"
              aria-selected={level === current}
              disabled={locked || closing}
              title={level === "auto" ? autoTitle : undefined}
              className={`modelsw__item ${level === current ? "modelsw__item--current" : ""}`}
              onClick={() => pick(level)}
            >
              <span className="modelsw__model">{level}</span>
              {level === current && <Check size={13} className="modelsw__check" />}
            </button>
          ))}
        </div> : <div ref={menuRef} className="effortsw__default" role="dialog" aria-label={t("status.effortTitle")}>
          <p>{t("status.effortModelDefaultHint")}</p>
          {onConfigure && <button type="button" disabled={locked || closing} className="modelsw__item" onClick={() => closeMenu(onConfigure)}>
            {t("settings.modelsProviders")}
          </button>}
        </div>}
      </AnchoredPopover>
    </div>
  );
}
