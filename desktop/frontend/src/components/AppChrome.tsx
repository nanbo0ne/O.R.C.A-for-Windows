import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { Bot, CalendarClock, Download, FolderOpen, GitBranch, Globe, Library, Minus, PanelLeft, PanelRight, Search, Settings, Square, SquarePen, Sparkles, SquareStop, UserRound, X } from "lucide-react";
import { useT, type DictKey } from "../lib/i18n";
import { UNKNOWN_ACCOUNT_SUMMARY, type BetaAccountSummary } from "../lib/betaBridge";
import type { SettingsTab } from "../lib/types";
import { Tooltip } from "./Tooltip";
import { AccountCapsule } from "./AccountCapsule";
import { Quit, WindowMinimise, WindowToggleMaximise } from "../../wailsjs/runtime/runtime";

type DesktopPlatform = "darwin" | "windows" | "linux";

// What the header capsule needs: which connections exist and how each stands.
interface BetaAccountView { account: boolean; web: boolean; summary: BetaAccountSummary }

interface AppChromeProps {
  uiStyle?: "modern" | "classic";
  platform: DesktopPlatform;
  browserPreviewChrome: boolean;
  browserAvailable: boolean;
  betaConnectionsAvailable?: boolean;
  betaAccount?: BetaAccountView;
  onOpenBetaConnections?: () => void;
  browserTaskState?: string;
  onStopBrowserTask?: () => void;
  sidebarTogglePressed: boolean;
  sidebarExpandBlocked: boolean;
  sidebarCollapsed: boolean;
  sidebarToggleTitle: string;
  workspacePanelMaximized: boolean;
  workspacePanelRenderable: boolean;
  workspaceTogglePressed: boolean;
  workspacePanelLabel: string;
  workspaceLabel: string;
  sessionLabel: string;
  modelLabel: string;
  statusLights: Array<{ key: string; label: string; tone: "neutral" | "info" | "success" | "warn" | "danger"; active: boolean }>;
  onOpenProject: () => void;
  onOpenView: () => void;
  onOpenSkills: () => void;
  onOpenBots: () => void;
  onOpenAutomations: () => void;
  onOpenToolLibrary: () => void;
  onToggleSidebar: () => void;
  onToggleWorkspacePanel: () => void;
  onOpenPalette: () => void;
  onNewSession?: () => void;
  onExportSession?: () => void;
  onCloseSession?: () => void;
  onOpenSettings?: (tab: SettingsTab) => void;
  onViewChanges?: () => void;
  onOpenBrowser?: () => void;
}

function ClassicAppChrome({
  platform,
  browserPreviewChrome,
  browserAvailable,
  betaConnectionsAvailable,
  betaAccount,
  onOpenBetaConnections,
  browserTaskState,
  onStopBrowserTask,
  sidebarTogglePressed,
  sidebarExpandBlocked,
  sidebarCollapsed,
  sidebarToggleTitle,
  workspacePanelMaximized,
  workspacePanelRenderable,
  workspaceTogglePressed,
  workspacePanelLabel,
  statusLights,
  onOpenProject,
  onOpenView,
  onOpenSkills,
  onOpenBots,
  onOpenAutomations,
  onOpenToolLibrary,
  onToggleSidebar,
  onToggleWorkspacePanel,
  onOpenPalette,
  onNewSession: _onNewSession,
  onExportSession: _onExportSession,
  onCloseSession: _onCloseSession,
  onOpenSettings: _onOpenSettings,
	onViewChanges: _onViewChanges,
  onOpenBrowser: _onOpenBrowser,
}: AppChromeProps) {
  const t = useT();
  const darwinChrome = platform === "darwin";
  // Classic uses the native Windows frame. Preview controls remain useful in
  // browser screenshots, where no native frame exists.
  const showWindowsControls = browserPreviewChrome && platform === "windows";
  const chromeClassName = [
    "app-chrome",
    "app-chrome--tabs",
    darwinChrome ? "app-chrome--darwin-tabs" : "app-chrome--native-tabs",
    !darwinChrome ? "app-chrome--identityless" : "",
    showWindowsControls ? "app-chrome--windows-controls" : "",
    `app-chrome--platform-${platform}`,
  ].filter(Boolean).join(" ");

  return (
    <header className={chromeClassName}>
      {browserPreviewChrome && darwinChrome && (
        <div className="app-chrome__traffic" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
      )}
      {darwinChrome && <span className="app-chrome__drag-rail" aria-hidden="true" />}
      <button
        className={[
          "app-chrome__panel-toggle",
          "app-chrome__panel-toggle--left",
          sidebarTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          sidebarExpandBlocked ? "app-chrome__panel-toggle--blocked" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={sidebarExpandBlocked ? undefined : onToggleSidebar}
        aria-label={sidebarToggleTitle}
        aria-pressed={!sidebarCollapsed}
        aria-disabled={sidebarExpandBlocked}
      >
        <PanelLeft size={16} />
      </button>

      <div className="app-chrome__drag-surface" aria-hidden="true" />
      <nav className="app-chrome__actions" aria-label={t("topbar.actions")}>
        <Tooltip label={t("topbar.openProject")}>
          <button type="button" className="app-chrome__action app-chrome__action--core" onClick={onOpenProject} aria-label={t("topbar.openProject")}>
            <FolderOpen size={14} />
            <span>{t("topbar.openProject")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topbar.view")}>
          <button type="button" className="app-chrome__action app-chrome__action--core" onClick={onOpenView} aria-label={t("topbar.view")}>
            <Square size={14} />
            <span>{t("topbar.view")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topbar.skills")}>
          <button type="button" className="app-chrome__action app-chrome__action--secondary" onClick={onOpenSkills} aria-label={t("topbar.skills")}>
            <Sparkles size={14} />
            <span>{t("topbar.skills")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topbar.botStatus")}>
          <button type="button" className="app-chrome__action app-chrome__action--secondary" onClick={onOpenBots} aria-label={t("topbar.botStatus")}>
            <Bot size={14} />
            <span>{t("topbar.bot")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topbar.automationStatus")}>
          <button type="button" className="app-chrome__action app-chrome__action--tertiary" onClick={onOpenAutomations} aria-label={t("topbar.automationStatus")}>
            <CalendarClock size={14} />
            <span>{t("topbar.automation")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("topbar.toolLibraryStatus")}>
          <button type="button" className="app-chrome__action app-chrome__action--tertiary" onClick={onOpenToolLibrary} aria-label={t("topbar.toolLibraryStatus")}>
            <Library size={14} />
            <span>{t("topbar.toolLibrary")}</span>
          </button>
        </Tooltip>
      </nav>

      <div className={`app-chrome__drag-surface app-chrome__drag-surface--center${betaConnectionsAvailable ? " beta-entry-slot" : ""}`} aria-hidden={!betaConnectionsAvailable || undefined}>
        {betaConnectionsAvailable && <BetaConnectionsEntry onClick={onOpenBetaConnections} account={betaAccount} />}
      </div>

      <div className="app-chrome__status app-chrome__status--primary" aria-label={t("topbar.statusLights")}>
        {statusLights.map((light) => (
          <Tooltip key={light.key} label={light.label}>
            <span className={`app-chrome__status-light app-chrome__status-light--${light.tone}${light.active ? " app-chrome__status-light--active" : ""}`} />
          </Tooltip>
        ))}
      </div>

      <div className={[
          "app-chrome__tools",
          workspaceTogglePressed ? "app-chrome__tools--workspace-pressed" : "",
        ].filter(Boolean).join(" ")}
        aria-label={t("tabBar.commandSearch")}
      >
        <button
          className="tabbar__command tabbar__command--compact app-chrome__command"
          type="button"
          onClick={onOpenPalette}
          aria-label={t("palette.placeholder")}
        >
          <Search size={13} className="tabbar__command-icon" />
          <span className="tabbar__command-text tabbar__command-text--full">{t("tabBar.commandSearch")}</span>
          <span className="tabbar__command-text tabbar__command-text--compact">{t("tabBar.commandSearchCompact")}</span>
          <kbd className="tabbar__command-kbd">Ctrl+K</kbd>
        </button>
      </div>

      {!workspacePanelMaximized && browserAvailable && (
        <button className="app-chrome__panel-toggle app-chrome__browser-toggle" type="button" onClick={_onOpenBrowser} aria-label={t("rightDock.browser")} title={t("rightDock.browser")}><Globe size={15} /></button>
      )}
      {browserTaskState && <div className="browser-task-status" role="status"><span>{t("browser.aiRunning", { state: browserTaskState })}</span><button type="button" title={t("browser.stopTask")} aria-label={t("browser.stopTask")} onClick={onStopBrowserTask}><SquareStop size={14} /></button></div>}
      {!workspacePanelMaximized && (
        <button
          className={[
            "app-chrome__panel-toggle",
            "app-chrome__panel-toggle--right",
            workspacePanelRenderable ? "app-chrome__panel-toggle--active" : "",
            workspaceTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          ].filter(Boolean).join(" ")}
          type="button"
          onClick={onToggleWorkspacePanel}
          aria-label={workspacePanelLabel}
          aria-pressed={workspacePanelRenderable}
        >
          <PanelRight size={16} />
        </button>
      )}
      {showWindowsControls && (
        <div className="app-chrome__window-controls app-chrome__window-controls--windows" aria-hidden="true">
          <span className="app-chrome__window-control app-chrome__window-control--minimize">
            <Minus size={12} strokeWidth={1.9} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--maximize">
            <Square size={10} strokeWidth={1.8} />
          </span>
          <span className="app-chrome__window-control app-chrome__window-control--close">
            <X size={12} strokeWidth={1.9} />
          </span>
        </div>
      )}
    </header>
  );
}

type ModernMenuKey = "file" | "project" | "tools" | "settings";

function ModernMenu({
  label, menuKey, open, summaryRef, children, onOpen, onClose, onMove,
}: {
  label: string;
  menuKey: ModernMenuKey;
  open: boolean;
  summaryRef: RefObject<HTMLElement | null>;
  children: ReactNode;
  onOpen: (key: ModernMenuKey) => void;
  onClose: () => void;
  onMove: (key: ModernMenuKey, direction: -1 | 1) => void;
}) {
  const focusItem = (edge: "first" | "last") => requestAnimationFrame(() => {
    const items = Array.from(summaryRef.current?.parentElement?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || []);
    items[edge === "first" ? 0 : items.length - 1]?.focus();
  });
  const handleSummaryKey = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "ArrowDown" || event.key === "Enter" || event.key === " ") {
      event.preventDefault(); onOpen(menuKey); focusItem("first");
    } else if (event.key === "ArrowUp") {
      event.preventDefault(); onOpen(menuKey); focusItem("last");
    } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault(); onMove(menuKey, event.key === "ArrowLeft" ? -1 : 1);
    } else if (event.key === "Escape") {
      event.preventDefault(); onClose();
    }
  };
  const handlePanelKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
    const index = items.indexOf(document.activeElement as HTMLButtonElement);
    if (event.key === "Escape") { event.preventDefault(); onClose(); summaryRef.current?.focus(); return; }
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault(); onMove(menuKey, event.key === "ArrowLeft" ? -1 : 1); return;
    }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault(); items[event.key === "Home" ? 0 : items.length - 1]?.focus(); return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      items[(index + direction + items.length) % items.length]?.focus();
    }
  };
  return (
    <details className="modern-chrome__menu" open={open}>
      <summary ref={summaryRef} aria-haspopup="menu" aria-expanded={open} onClick={(event) => { event.preventDefault(); open ? onClose() : onOpen(menuKey); }} onKeyDown={handleSummaryKey}>{label}</summary>
      <div className="modern-chrome__menu-panel" role="menu" onKeyDown={handlePanelKey}>{children}</div>
    </details>
  );
}

function ModernMenuItem({ children, onClick, onClose, icon }: { children: ReactNode; onClick?: () => void; onClose: () => void; icon?: ReactNode }) {
  return <button type="button" role="menuitem" className="modern-chrome__menu-item" onClick={() => { onClose(); onClick?.(); }}>{icon}<span>{children}</span></button>;
}

function ModernAppChrome(props: AppChromeProps) {
  const {
    platform, browserPreviewChrome, browserAvailable, browserTaskState, onStopBrowserTask, sidebarTogglePressed: _sidebarTogglePressed, sidebarExpandBlocked,
    sidebarCollapsed, sidebarToggleTitle, workspacePanelMaximized,
    workspacePanelRenderable, workspaceTogglePressed, workspacePanelLabel,
    workspaceLabel, sessionLabel, modelLabel, onOpenProject, onOpenView,
    onOpenSkills, onOpenBots, onOpenAutomations, onOpenToolLibrary,
    onToggleSidebar, onToggleWorkspacePanel, onOpenPalette, onNewSession,
    onExportSession, onCloseSession, onOpenSettings, onViewChanges,
    onOpenBrowser, betaConnectionsAvailable, onOpenBetaConnections,
  } = props;
  const t = useT();
  const showWindowsControls = platform === "windows";
  const [openMenu, setOpenMenu] = useState<ModernMenuKey | null>(null);
  const chromeRef = useRef<HTMLElement | null>(null);
  const fileRef = useRef<HTMLElement | null>(null);
  const projectRef = useRef<HTMLElement | null>(null);
  const toolsRef = useRef<HTMLElement | null>(null);
  const settingsRef = useRef<HTMLElement | null>(null);
  const menuRefs: Record<ModernMenuKey, RefObject<HTMLElement | null>> = { file: fileRef, project: projectRef, tools: toolsRef, settings: settingsRef };
  const moveMenu = (current: ModernMenuKey, direction: -1 | 1) => {
    const keys: ModernMenuKey[] = ["file", "project", "tools", "settings"];
    const next = keys[(keys.indexOf(current) + direction + keys.length) % keys.length];
    setOpenMenu(next);
    requestAnimationFrame(() => menuRefs[next].current?.focus());
  };
  useEffect(() => {
    if (!openMenu) return;
    const closeOutside = (event: PointerEvent) => { if (!chromeRef.current?.contains(event.target as Node)) setOpenMenu(null); };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [openMenu]);
  return (
    <header ref={chromeRef} className={["app-chrome", "modern-chrome", `app-chrome--platform-${platform}`].join(" ")}>
      <div className="modern-chrome__menu-row">
        <nav className="modern-chrome__menus" aria-label="Application menu">
          <ModernMenu label={t("modernMenu.file")} menuKey="file" open={openMenu === "file"} summaryRef={fileRef} onOpen={setOpenMenu} onClose={() => setOpenMenu(null)} onMove={moveMenu}>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onNewSession} icon={<SquarePen size={15} />} >{t("topbar.newSession")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenProject} icon={<FolderOpen size={15} />}>{t("topbar.openProject")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onExportSession} icon={<Download size={15} />}>{t("modernMenu.exportSession")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onCloseSession} icon={<X size={15} />}>{t("modernMenu.closeSession")}</ModernMenuItem>
          </ModernMenu>
          <ModernMenu label={t("modernMenu.project")} menuKey="project" open={openMenu === "project"} summaryRef={projectRef} onOpen={setOpenMenu} onClose={() => setOpenMenu(null)} onMove={moveMenu}>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenProject} icon={<FolderOpen size={15} />}>{t("modernMenu.switchProject")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenView} icon={<Square size={15} />}>{t("modernMenu.openWorkspace")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onViewChanges} icon={<GitBranch size={15} />}>{t("modernMenu.viewChanges")}</ModernMenuItem>
          </ModernMenu>
          <ModernMenu label={t("modernMenu.tools")} menuKey="tools" open={openMenu === "tools"} summaryRef={toolsRef} onOpen={setOpenMenu} onClose={() => setOpenMenu(null)} onMove={moveMenu}>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenPalette} icon={<Search size={15} />}>{t("modernMenu.commandPalette")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenSkills} icon={<Sparkles size={15} />}>{t("topbar.skills")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenBots} icon={<Bot size={15} />}>{t("topbar.bot")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenAutomations} icon={<CalendarClock size={15} />}>{t("topbar.automation")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={onOpenToolLibrary} icon={<Library size={15} />}>{t("topbar.toolLibrary")}</ModernMenuItem>
          </ModernMenu>
          <ModernMenu label={t("modernMenu.settings")} menuKey="settings" open={openMenu === "settings"} summaryRef={settingsRef} onOpen={setOpenMenu} onClose={() => setOpenMenu(null)} onMove={moveMenu}>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={() => onOpenSettings?.("general")} icon={<Settings size={15} />}>{t("modernMenu.general")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={() => onOpenSettings?.("models")}>{t("modernMenu.models")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={() => onOpenSettings?.("permissions")}>{t("modernMenu.permissions")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={() => onOpenSettings?.("computer")}>{t("modernMenu.computer")}</ModernMenuItem>
            <ModernMenuItem onClose={() => setOpenMenu(null)} onClick={() => onOpenSettings?.("appearance")}>{t("modernMenu.appearance")}</ModernMenuItem>
          </ModernMenu>
        </nav>
        {betaConnectionsAvailable && <div className="modern-chrome__beta-entry"><BetaConnectionsEntry onClick={onOpenBetaConnections} /></div>}
        <div className="modern-chrome__drag" aria-hidden="true" />
        {showWindowsControls && <WindowControls browserPreviewChrome={browserPreviewChrome} />}
      </div>
      <div className="modern-chrome__workspace-row">
        <button className="modern-chrome__sidebar-toggle" type="button" onClick={sidebarExpandBlocked ? undefined : onToggleSidebar} aria-label={sidebarToggleTitle} aria-pressed={!sidebarCollapsed} disabled={sidebarExpandBlocked}><PanelLeft size={16} /></button>
        <div className="modern-chrome__brand" aria-label="O.R.C.A.">O.R.C.A.</div>
        <AccountCapsule />
        <div className="modern-chrome__context"><span title={workspaceLabel}>{workspaceLabel}</span><span aria-hidden="true">/</span><strong title={sessionLabel}>{sessionLabel}</strong></div>
        <div className="modern-chrome__workspace-actions">
          <span className="modern-chrome__model" title={modelLabel}>{modelLabel}</span>
		  <button className="modern-chrome__icon-button" type="button" onClick={onExportSession} aria-label={t("modernMenu.exportSession")}><Download size={15} /></button>
		  <button className="modern-chrome__icon-button" type="button" onClick={onOpenView} aria-label={t("modernMenu.openWorkspace")}><FolderOpen size={15} /></button>
          {browserAvailable && <button className="modern-chrome__icon-button" type="button" onClick={onOpenBrowser} aria-label={t("rightDock.browser")} title={t("rightDock.browser")}><Globe size={15} /></button>}
          {browserTaskState && <div className="browser-task-status" role="status"><span>{t("browser.aiRunning", { state: browserTaskState })}</span><button type="button" title={t("browser.stopTask")} aria-label={t("browser.stopTask")} onClick={onStopBrowserTask}><SquareStop size={14} /></button></div>}
          <button className="modern-chrome__icon-button" type="button" onClick={onOpenPalette} aria-label={t("tabBar.commandSearch")}><Search size={15} /></button>
          {!workspacePanelMaximized && <button className={`modern-chrome__icon-button${workspaceTogglePressed ? " is-active" : ""}`} type="button" onClick={onToggleWorkspacePanel} aria-label={workspacePanelLabel} aria-pressed={workspacePanelRenderable}><PanelRight size={15} /></button>}
        </div>
      </div>
    </header>
  );
}

function WindowControls({ browserPreviewChrome }: { browserPreviewChrome: boolean }) {
  const t = useT();
  return <div className="app-chrome__window-controls app-chrome__window-controls--windows">
    <button type="button" className="app-chrome__window-control app-chrome__window-control--minimize" aria-label={t("topbar.windowMinimize")} onClick={() => { if (!browserPreviewChrome) WindowMinimise(); }}><Minus size={12} strokeWidth={1.9} /></button>
    <button type="button" className="app-chrome__window-control app-chrome__window-control--maximize" aria-label={t("topbar.windowMaximize")} onClick={() => { if (!browserPreviewChrome) WindowToggleMaximise(); }}><Square size={10} strokeWidth={1.8} /></button>
    <button type="button" className="app-chrome__window-control app-chrome__window-control--close" aria-label={t("topbar.windowClose")} onClick={() => { if (!browserPreviewChrome) Quit(); }}><X size={12} strokeWidth={1.9} /></button>
  </div>;
}

function BetaConnectionsEntry({ onClick, account }: { onClick?: () => void; account?: BetaAccountView }) {
  const t = useT();
  const summary = account?.summary ?? UNKNOWN_ACCOUNT_SUMMARY;
  const orcaKey: DictKey = summary.orca === "signed_in" ? "beta.signedIn"
    : summary.orca === "signing_in" ? "beta.authorizing"
    : summary.orca === "restoring" || summary.orca === "unknown" ? "beta.checking"
    : summary.orca === "logging_out" ? "beta.loggingOut"
    : summary.orca === "error" ? "beta.attention"
    : "beta.login";
  const webKey: DictKey | null = !account?.web ? null
    : summary.web === "connected" ? "beta.connected"
    : summary.web === "retained" ? "beta.signedIn"
    : summary.web === "attention" ? "beta.attention"
    : summary.web === "idle" ? "beta.notConnected"
    : "beta.checking";
  // A signed-in account is identified by name, not by the word "signed in": the
  // capsule has room for one and the name answers "who am I here" at a glance.
  const orcaText = summary.orca === "signed_in" && summary.orcaName ? summary.orcaName : t(orcaKey);
  const signedIn = summary.orca === "signed_in";
  const attention = summary.orca === "error" || summary.web === "attention";
  const description = [t("beta.account") + "：" + t(orcaKey), account?.web ? t("beta.web") + "：" + t(webKey ?? "beta.loading") : ""].filter(Boolean).join(" · ");
  return <button
    type="button"
    className={["beta-connections-entry", signedIn ? "beta-connections-entry--signed-in" : "beta-connections-entry--signed-out", attention ? "beta-connections-entry--attention" : ""].filter(Boolean).join(" ")}
    data-beta-entry
    data-beta-orca={summary.orca}
    data-beta-web={account?.web ? summary.web : "off"}
    aria-label={description}
    title={description}
    onClick={onClick}
  >
    <span className="beta-connections-entry__orca"><UserRound size={13} /><span className="beta-connections-entry__text">{orcaText}</span></span>
    {webKey && <span className="beta-connections-entry__web" aria-hidden="true"><Globe size={12} /><span className="beta-connections-entry__text">{t(webKey)}</span></span>}
  </button>;
}

export function AppChrome(props: AppChromeProps) {
  return props.uiStyle === "classic" ? <ClassicAppChrome {...props} /> : <ModernAppChrome {...props} />;
}
