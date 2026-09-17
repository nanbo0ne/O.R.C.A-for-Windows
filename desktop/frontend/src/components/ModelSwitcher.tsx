import { useCallback, useEffect, useRef, useState } from "react";
import { Brain, Check, ChevronsUpDown } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { modelDisplayLabel, selectableModels } from "../lib/modelCatalog";
import { useT } from "../lib/i18n";
import type { ModelInfo } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";
import { Tooltip } from "./Tooltip";

// ModelSwitcher opens an upward popover listing configured providers. Selecting
// one switches the active model while the current conversation continues.
export function ModelSwitcher({ label, tabId, onPick, showTooltip = false }: { label: string; tabId?: string; onPick: (name: string, displayLabel?: string) => void; showTooltip?: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const triggerWidth = triggerRef.current?.getBoundingClientRect().width;

  const clearCloseTimer = useCallback(() => {
    if (closeTimerRef.current === null) return;
    window.clearTimeout(closeTimerRef.current);
    closeTimerRef.current = null;
  }, []);

  const openMenu = useCallback(() => {
    clearCloseTimer();
    setClosing(false);
    setOpen(true);
  }, [clearCloseTimer]);

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

  useEffect(() => {
    if (open) {
      (tabId ? app.ModelsForTab(tabId) : app.Models()).then((next) => setModels(selectableModels(asArray(next)))).catch(() => {});
    }
  }, [open, tabId]);

  useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

  useEffect(() => {
    if (!open || closing) return;
    const frame = requestAnimationFrame(() => {
      if (document.activeElement === triggerRef.current) {
        (menuRef.current?.querySelector<HTMLButtonElement>('[aria-selected="true"]') || menuRef.current?.querySelector<HTMLButtonElement>("button"))?.focus();
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [open, closing, models]);

  const pick = (name: string, displayLabel?: string) => {
    onPick(name, displayLabel);
    closeMenu();
  };

  const trigger = (
      <button
        ref={triggerRef}
        type="button"
        className="modelsw__trigger"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open && !closing}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); openMenu(); }
        }}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <Brain size={13} className="modelsw__kind" />
        <span className="modelsw__label">{label}</span>
        <ChevronsUpDown size={11} />
      </button>
  );

  return (
    <div className="modelsw">
      {showTooltip ? <Tooltip label={label} fill disabled={open || closing}>{trigger}</Tooltip> : trigger}
      <AnchoredPopover
        open={open}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="modelsw__menu modelsw__menu--portal"
        style={{ minWidth: triggerWidth ? Math.max(triggerWidth, 160) : undefined, maxWidth: 400 }}
      >
        <div ref={menuRef} role="listbox" aria-label={label} onKeyDown={(event) => {
          if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
          const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"));
          if (!buttons.length) return;
          event.preventDefault();
          const currentIndex = buttons.indexOf(document.activeElement as HTMLButtonElement);
          const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (currentIndex + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
          buttons[next].focus();
        }}>
          {models.length === 0 && <div className="modelsw__empty">{t("status.noModels")}</div>}
          {models.map((m) => (
            <button
              key={m.ref}
              type="button"
              role="option"
              aria-selected={m.current}
              className={`modelsw__item ${m.current ? "modelsw__item--current" : ""}`}
              onClick={() => pick(m.ref, modelDisplayLabel(m.ref, m.model, m.metadataSource === "deepseek_official"))}
            >
              <span className="modelsw__copy">
                <span className="modelsw__model" title={m.ref}>{modelDisplayLabel(m.ref, m.model, m.metadataSource === "deepseek_official")}</span>
                <span className="modelsw__provider" title={providerLabel(m, t)}>{providerLabel(m, t)}</span>
              </span>
              {m.current && <Check size={13} className="modelsw__check" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function providerLabel(model: ModelInfo, t: ReturnType<typeof useT>): string {
  const provider = model.provider;
  switch (provider) {
    case "deepseek":
    case "deepseek-flash":
    case "deepseek-pro":
      return model.metadataSource === "deepseek_official" ? t("settings.providerLabel.deepseek") : provider;
    case "mimo-api":
    case "mimo":
    case "xiaomi-mimo":
      return t("settings.providerLabel.mimoApi");
    case "mimo-token-plan":
    case "mimo-pro":
    case "mimo-flash":
      return t("settings.providerLabel.mimoTokenPlan");
    default:
      return provider;
  }
}
