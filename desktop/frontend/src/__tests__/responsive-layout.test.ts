import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
// Source contracts must not depend on Git's platform-specific checkout newlines.
const readSource = (...path: string[]) => readFileSync(join(root, ...path), "utf8").replace(/\r\n/g, "\n");
const css = readSource("styles.css");
const statusBar = readSource("components", "StatusBar.tsx");
const chrome = readSource("components", "AppChrome.tsx");
const app = readSource("App.tsx");
const composer = readSource("components", "Composer.tsx");
const settings = readSource("components", "SettingsPanel.tsx");
const processCard = readSource("components", "ProcessCard.tsx");
const transcript = readSource("components", "Transcript.tsx");
const todoPanel = readSource("components", "TodoPanel.tsx");
const promptShelf = readSource("components", "PromptShelf.tsx");
const toolCard = readSource("components", "ToolCard.tsx");
const controller = readSource("lib", "useController.ts");

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nresponsive layout safeguards");
check(
  css.includes(':root[data-ui-style="modern"] .transcript-shell:has(> .jump-bar) > .transcript') &&
    css.includes("padding-inline: 48px;") &&
    css.includes(':root[data-ui-style="modern"] .jump-item {') && css.includes("max-width: 20px;"),
  "modern navigation has a reserved gutter and hover markers cannot grow into text",
);
check(!css.includes("padding: 5px 120px"), "composer has no fixed right-side reservation");
check(!css.includes(".composer-card__actions {\n    right:"), "composer actions stay in grid flow");
check(chrome.includes("function ClassicAppChrome") && chrome.includes("function ModernAppChrome"), "chrome uses explicit presentation branches");
check(
  app.includes('{desktopUIStyle === "classic" && (\n          <header className="topicbar">'),
  "modern chrome does not stack the legacy TopicBar as a third header row",
);
check(css.includes("@media (max-width: 1100px)") && css.includes("@media (max-width: 780px)"), "chrome priorities have narrow breakpoints");
check(
  ["modernMenu.file", "modernMenu.project", "modernMenu.tools", "modernMenu.settings"].every((key) => chrome.includes(`t("${key}")`)) &&
    chrome.includes("onOpenSkills") && chrome.includes("onOpenBots") && chrome.includes("onOpenAutomations") && chrome.includes("onOpenToolLibrary"),
  "modern text menus keep all secondary commands available",
);
check(
  css.includes(':root[data-ui-style="modern"] .modern-chrome__workspace-actions') &&
    css.includes(':root[data-ui-style="modern"] .modern-chrome__icon-button'),
  "modern workspace actions keep stable icon slots",
);
check(app.includes("topicbar__overflow-menu") && css.includes(".topicbar__action--direct-utility"), "topic actions expose a narrow overflow menu");
check(css.includes(".statusbar {\n    max-width: none;\n    gap: 6px;\n    overflow: hidden"), "narrow status bar cannot wrap or overflow");
check(
  statusBar.includes('uiStyle === "classic" ? "statusbar__details--classic"') &&
    css.includes("container-name: statusbar;") &&
    css.includes("@container statusbar (max-width: 760px)") &&
    css.includes(".statusbar__details--classic") &&
    css.includes(".statusbar:has(.statusbar__details--classic[open])") &&
    css.includes(".statusbar__group--primary,") &&
    css.includes(".statusbar__compact"),
  "classic status metrics compact by statusbar container and retain secondary details",
);
check(
  statusBar.includes("function currencySymbol(currency?: string): string | null") &&
    statusBar.includes("if (!value) return null;") &&
    statusBar.includes("return value;") &&
    statusBar.includes("costAvailable && costLabel !== null"),
  "unknown or missing currencies never masquerade as RMB in the status bar",
);
check(
  css.includes(".footer-shelves > .todobar {\n  flex: 0 0 auto;\n  width: min(520px, 100%);") &&
    css.includes("min-width: min(300px, 100%);\n  max-width: 100%;\n  margin-inline: auto;"),
  "Todo stays centered and constrained by the conversation footer",
);
check(
  settings.includes('<SettingsField label={t("settings.autoCheckUpdates")}') &&
    settings.includes('<div className="settings-update-control">') &&
    !settings.includes('<SettingsField label={t("settings.checkUpdatesNow")}'),
  "manual update check stays beside the automatic update toggle",
);
check(
  app.includes("app.GetProductCapabilities()") &&
    composer.includes("promptModes.map((mode)") &&
    !composer.includes("PROMPT_MODE_OPTIONS") &&
    !composer.includes("CircleHelp") &&
    !composer.includes("promptModeHelpKey"),
  "prompt modes come from product capabilities without descriptions or a help button",
);
check(
  app.includes("if (!activeTabId || state.running) return;") &&
    app.includes("pendingPromptModesByTab, activeTabId") &&
    app.includes("void applyPendingRuntimePrefs(activeTabId);") &&
    app.includes("pendingPromptModeSwitchRef.current[activeTabId]"),
  "a confirmed running mode switch applies as soon as the turn becomes idle",
);
check(
  app.includes("promptModeSwitchFailedByTab") &&
    app.includes("promptModeSwitchFailed || state.approval") &&
    app.includes("setPromptModeSwitchFailedByTab((current) => ({ ...current, [tabId]: true }))") &&
    app.indexOf("submitPromptToAgent(nextPrompt.displayText, nextPrompt.submitText).then") <
      app.indexOf("const rest = queue.slice(1)", app.indexOf("submitPromptToAgent(nextPrompt.displayText, nextPrompt.submitText).then")),
  "failed mode switches retain queued prompts until a successful retry",
);
check(
  !settings.includes('<SettingsField label={t("settings.botModel")}') &&
    !settings.includes('<SettingsField label={t("settings.botPromptMode")}') &&
    !settings.includes('<SettingsField label={t("settings.botWorkspaceRoot")}') &&
    settings.includes("productCapabilities.automationWorkspaceEnabled === true"),
  "bot and memory settings follow the product capability boundary",
);
check(
  app.includes("promptModes={automationConversation ? [] : productCapabilities.promptModes}") &&
    app.includes("promptModeLocked={false}") &&
    app.includes("showToolApprovalControls={!automationConversation}") &&
    composer.includes("showToolApprovalControls && uiStyle === \"modern\"") &&
    composer.includes("showToolApprovalControls && uiStyle === \"classic\"") &&
    composer.includes("disabled={disabled || promptModeLocked}"),
  "Orca hides both the ordinary mode selector and redundant approval selector",
);
const chooser = readSource("components", "NewSessionChooser.tsx");
const projectTree = readSource("components", "ProjectTree.tsx");
check(
  !chooser.includes('choose("automation"') &&
    projectTree.includes('node.kind === "orca_topic"') &&
    !projectTree.includes('node.kind === "automation_folder"') &&
    !projectTree.includes("automation_history_folder"),
  "Orca is a fixed top-level entry rather than a creatable workspace",
);
check(
  projectTree.includes('node.kind === "orca_topic" ? "automation"') &&
    projectTree.includes("node.readOnly || node.primary ? []"),
  "Orca stays fixed instead of inheriting project rename and drag actions",
);
const composerContract = css.slice(css.indexOf("/* Composer responsive contract."));
check(
  [720, 580, 460, 380, 320].every((width, index, widths) => {
    const position = composerContract.indexOf(`@container (max-width: ${width}px)`);
    const previous = index === 0 ? -1 : composerContract.indexOf(`@container (max-width: ${widths[index - 1]}px)`);
    return position > previous;
  }),
  "final Composer breakpoints use one descending 720/580/460/380/320 sequence",
);
check(
  composerContract.includes(".composer-meta__control--model {\n    display: inline-flex;") &&
    composerContract.includes(".composer-enhanced__button svg:last-child {\n    display: none;"),
  "narrow Composer retains model access and collapses mode trigger to one icon",
);
check(
  css.includes('grid-template-areas: "input input input" "meta status actions";') &&
    css.includes("grid-template-columns: max-content minmax(0, 1fr) max-content;") &&
    css.includes("@container (max-width: 320px)") &&
    css.includes(':root[data-ui-style="modern"] .composer-wrap--modern .composer-card__actions--modern {\n  display: flex;') &&
    composer.indexOf('composer-modern-parameter--effort') < composer.indexOf('composer-modern-parameter--model'),
  "Modern footer separates flexible status from right-aligned effort/model actions",
);
check(
  css.includes(':root[data-ui-style="modern"] .transcript {\n  width: 100%;\n  max-width: none;') &&
    !css.includes(':root[data-ui-style="modern"] .main__scroll,\n:root[data-ui-style="modern"] .transcript,'),
  "Modern transcript keeps a full-width scroll viewport around the reading column",
);
check(
  composer.includes('className="composer-modern-status" role="status"') &&
    css.includes("grid-area: status;") &&
    css.includes("justify-self: stretch;") &&
    css.includes("height: 30px;") &&
    css.includes("line-height: 30px;") &&
    composer.includes("onClick={focusComposerSurface}") &&
    !composer.includes("<Tooltip label={runActivity} fill>") &&
    !css.includes("max-width: 148px;\n  flex: 0 1 auto;"),
  "Modern run status owns flexible space independently of the fixed buttons without a floating tooltip",
);
check(
  composer.includes("const nativeClipboardPasteInFlightRef = useRef(false)") &&
    composer.includes("native-clipboard-hash:") &&
    composer.includes("if (files.some((file) => file.type.toLowerCase().startsWith(\"image/\"))) return;") &&
    composer.includes("nativeClipboardPasteInFlightRef.current = false"),
  "image paste has one asynchronous native path and content-based duplicate protection",
);
check(
  composer.includes("// Enter queues while the agent is running") &&
    composer.includes("if (e.key === \"Enter\" && !e.shiftKey && !composing)") &&
    composer.includes("Shift+Enter remains newline"),
  "Enter submits while Shift+Enter remains a textarea newline",
);
check(
  css.includes(':root[data-ui-style="classic"] .composer-runstatus {\n  width: min(360px, 40cqw);'),
  "Classic run status follows the Composer width when both sidebars are open",
);
check(
    composer.includes('composer-runstatus__primary--stop${cancelRequested') &&
    composer.includes("onClick={handleCancel}") &&
    composer.includes("disabled={cancelRequested && !cancelSlow}") &&
    composer.includes('<Square size={11} fill="currentColor" strokeWidth={1.8} />') &&
    css.includes(".composer-runstatus__primary {\n  --wails-draggable: no-drag;") &&
    css.includes("width: 34px;\n  min-width: 34px;\n  max-width: 34px;\n  height: 34px;") &&
    css.includes(".composer-runstatus__primary--send {") &&
    composerContract.includes(".composer-runstatus__primary {\n    width: 30px;\n    min-width: 30px;\n    max-width: 30px;") &&
    !composer.includes("composer-runstatus__primary-label"),
  "running mouse action stays Stop with stable geometry while drafts use keyboard send",
);
check(
  composer.includes("Plain text always follows the textarea's native paste path") &&
    composer.includes('if (pasted !== "") return;') &&
    !composer.includes("shouldFoldPaste") &&
    !composer.includes("composer__pasted"),
  "plain text paste remains editable text and wins over rich clipboard image hints",
);
check(
  app.includes('target.classList.contains("composer__input")') &&
    app.includes("pasteRequest={composerPasteRequest}") &&
    composer.includes("pasteFromContextMenu") &&
    composer.includes("await attachNativeClipboardImage(true)"),
  "custom context-menu paste delegates images and files to Composer",
);
check(
  css.includes(":root[data-theme-style] .composer__btn--send:disabled") &&
    css.includes("background: var(--control-disabled-bg)") &&
    css.includes("color: var(--control-disabled-fg)"),
  "themed disabled send button stays muted",
);
check(
  composer.includes("!hasSendableContent && !(goalModeOn && !activeGoal)") &&
    !composer.includes("!text.trim() && attachments.length === 0 && workspaceRefs.length === 0"),
  "failed attachments cannot leave the ordinary send button falsely enabled",
);
check(
  composer.includes("const COMPOSER_AUTO_MAX_LINES = 10") &&
    composer.includes("composerAutoInputMaxHeight(node)") &&
    composer.includes("node.scrollHeight > maxHeight + 1") &&
    css.includes(".composer__input {\n  flex: 1;\n  resize: none;\n  margin: 0;\n  padding: 0;"),
  "Composer grows with content up to a stable line limit before scrolling",
);
check(
  settings.includes('["compact", "detailed"]') &&
    !settings.includes('["compact", "standard", "detailed"]'),
  "process settings expose only compact and detailed modes",
);
check(
  processCard.includes("const closeFromKeyboard") &&
    processCard.includes("{hasBody && actualOpen && (") &&
    transcript.includes("processOpenOverrides.get(segment.id)") &&
    transcript.includes("next.set(segment.id, nextOpen)"),
  "compact process details can close cleanly and preserve stable segment overrides",
);
check(
  css.includes(".process-activity-mark") &&
    css.includes("prefers-reduced-motion: reduce") &&
    css.includes("process-activity-spinner--tool") &&
    css.includes("process-activity-spin-clockwise") &&
    css.includes("process-activity-spin-counterclockwise") &&
    !css.includes("animation-direction") &&
    transcript.includes("activityIndicatorPhase(items, activityIndicatorEnabled, running, paused)") &&
    transcript.includes("timeline-entry--activity"),
  "single activity ring regenerates for each direction and remains reduced-motion safe",
);
check(
  !css.includes(".composer-enhanced__button--switching svg:first-child") &&
    composer.includes("<RuntimeSwitchBar progress={runtimeSwitch} />") &&
    css.includes("width: min(180px, calc(100% - 16px));") &&
    css.includes("height: 3px;"),
  "mode switching keeps the selector still and shows a right-aligned stage bar",
);
check(
  composer.includes("<Pause size={15}") && composer.includes("<Play size={14}") &&
    !composer.includes("PauseCircle") && !composer.includes("PlayCircle") &&
    composer.includes('<Square size={11} fill="currentColor"'),
  "pause, resume, and stop share a consistent plain-icon language",
);
check(
  transcript.includes('case "mode_switch":') && transcript.includes("<ModeSwitchRow item={segment.item} />") &&
    controller.includes('kind: "mode_switch"') && controller.includes('type: "runtime_switch"'),
  "runtime mode switches render as standalone persistent timeline rows",
);
check(
  css.includes(".footer {\n  position: relative;") &&
    css.includes("border-top: 0;\n  background: transparent;") &&
    css.includes(".footer-shelves") &&
    css.includes("background: transparent;"),
  "footer shelves remain transparent outside their individual cards",
);
check(
  todoPanel.includes("AnchoredPopover") &&
    todoPanel.includes('className="todobar__surface"') &&
    todoPanel.includes('data-ui-surface="panel"') &&
    todoPanel.includes('className="todobar__details"') &&
    todoPanel.includes('placement="auto"') &&
    css.includes(".todo-popover"),
  "Todo keeps a stable in-flow trigger and opens details in an anchored portal",
);
check(
  promptShelf.includes('data-ui-surface="panel"') &&
    transcript.includes('data-ui-surface="panel"') &&
    !transcript.includes('className={`turn-stats-row turn-process-panel${open ? " turn-stats-row--open" : ""}`} data-ui-surface="panel"') &&
    !processCard.includes('data-ui-surface="panel"') &&
    !toolCard.includes('data-ui-surface="panel"'),
  "panel ownership stays on the shelf or turn while process and tool rows remain flat",
);
check(
  css.includes(".turn-process-panel {\n  width: min(100%, 820px);\n  overflow: visible;\n  border: 0;\n  border-radius: 0;\n  background: transparent;") &&
    !css.includes(".turn-process-panel > button"),
  "completed-turn summary uses the original transparent inline treatment",
);
check(
  toolCard.includes('data-ui-surface="content"') &&
    css.includes(":root[data-theme-style] .process-activity-rail .process-card__body {\n  padding: 0 4px 8px 25px;\n  border-top: 0;\n  background: transparent;"),
  "only tool output surfaces retain contained backgrounds inside the activity rail",
);
check(
  transcript.includes("transcript--hydrating") &&
    css.includes(".transcript--hydrating .timeline-entry") &&
    css.includes("animation: none !important;"),
  "restored history skips bulk entrance animation during its first paint",
);
const sessionLoader = controller.slice(controller.indexOf("const loadSessionDataForTab ="), controller.indexOf("const activeTabFromBackend ="));
const primaryLoaded = sessionLoader.indexOf('dispatchTo(tabId, { type: "session_primary_loaded"');
const primaryDependencies = sessionLoader.indexOf("await Promise.all([metaLoad, historyLoad])");
const auxiliaryEffort = sessionLoader.indexOf("void safe(refreshEffortForTab(tabId,");
check(
  controller.includes("Meta and history are the only first-paint dependencies") &&
    controller.includes("afterNextPaint()") &&
    controller.includes('dispatchTo(meta.id, { type: "session_load_start"') &&
    primaryDependencies >= 0 && primaryLoaded > primaryDependencies &&
    auxiliaryEffort > primaryLoaded &&
    sessionLoader.slice(auxiliaryEffort).includes("if (sessionLoadCurrent(tabId, seq)) dispatchTo(id, action)"),
  "conversation selection paints before history work and auxiliary status hydrates later",
);
check(
  controller.includes("const sessionLoads = useRef(new Map<string, Promise<void>>())") &&
    controller.includes("if (existing && !reset) {") &&
    controller.includes("const balanceRefreshSeq = useRef(new Map<string, number>())") &&
    controller.includes("if (balanceRefreshSeq.current.get(tabId) === seq) dispatchTo(tabId, { type: \"balance\", balance });") &&
    controller.includes("if (sessionLoads.current.get(tabId) === load) sessionLoads.current.delete(tabId)"),
  "concurrent tab loads coalesce and stale balance results cannot overwrite a newer model",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
