import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { sameComposerDraft, submitComposerDraft } from "../components/Composer";
import ts from "typescript";

let passed = 0;
let failed = 0;

function check(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const composerSource = readFileSync(fileURLToPath(new URL("../components/Composer.tsx", import.meta.url)), "utf8");
const appSource = readFileSync(fileURLToPath(new URL("../App.tsx", import.meta.url)), "utf8");
const controllerSource = readFileSync(fileURLToPath(new URL("../lib/useController.ts", import.meta.url)), "utf8");

const baseDraft = {
  tabId: "tab-a",
  text: "keep this draft",
  attachments: [],
  workspaceRefs: [],
  sessionRefs: [],
};
const changedText = { ...baseDraft, text: "new draft" };
const changedAttachment = { ...baseDraft, attachments: [{}] };
const changedTab = { ...baseDraft, tabId: "tab-b" };
check(sameComposerDraft(baseDraft, baseDraft), "unchanged draft can be cleared after success");
check(!sameComposerDraft(baseDraft, changedText), "text entered while sending is preserved");
check(!sameComposerDraft(baseDraft, changedAttachment), "attachments added while sending are preserved");
check(!sameComposerDraft(baseDraft, changedTab), "switching tabs prevents the old send from clearing the new draft");

const draft = {
  text: "keep this draft",
  attachments: [".orca/attachments/keep.png"],
};
let clearCalls = 0;
let reported: unknown;

const sent = await submitComposerDraft(
  async () => {
    throw new Error("workspace is still starting");
  },
  () => {
    clearCalls += 1;
    draft.text = "";
    draft.attachments = [];
  },
  (error) => {
    reported = error;
  },
);

console.log("\nComposer send failure");
check(sent === false, "rejecting backend reports an unsuccessful send");
check(clearCalls === 0, "rejecting backend does not clear the draft");
check(draft.text === "keep this draft" && draft.attachments.length === 1, "text and attachments remain available for retry");
check(reported instanceof Error && reported.message === "workspace is still starting", "failure is delivered to the inline error reporter");
check(composerSource.includes('role="status"') && composerSource.includes('t("msg.sendFailed")'), "Composer renders send failures as inline status feedback");
check(composerSource.includes("if (cancelledSubmissionRef.current || !sameComposerDraft(submittedDraft, draftSnapshotRef.current)) return;"), "successful sends clear only unchanged, uncancelled draft snapshots");
check(composerSource.includes("onClick={handleCancel}") && composerSource.includes("composer-runstatus__primary--stop") && !composerSource.includes("showDraftSend"), "running primary action stays Stop even with a new draft");
check(composerSource.includes("disabled={cancelRequested && !cancelSlow}") && !composerSource.includes("readOnly={submitting}"), "Stop remains available during Submit; slow cancellation permits retry and draft stays editable");
check(appSource.includes("await send(trimmed, submitText.trim())"), "App awaits the Submit callback before Composer can clear the draft");
check(controllerSource.includes("throw error;"), "controller send propagates the rejecting backend promise");
check(controllerSource.includes('if (!active?.id) throw new Error("Cannot send: no active tab is available.");'), "missing active tabs reject instead of reporting a successful send");
check(/className="composer__btn composer__btn--send"\s+type="button"\s+aria-label=\{t\("composer.send"\)\}/.test(composerSource), "idle send has an accessible name in native WebView2");

// Execute the component's actual callbacks with controlled refs and setters.
const parsed = ts.createSourceFile("Composer.tsx", composerSource, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
let cancelCallback: ts.Expression | undefined;
let clearCallback: ts.Expression | undefined;
function visit(node: ts.Node) {
  if (ts.isVariableDeclaration(node) && node.name.getText(parsed) === "handleCancel") cancelCallback = node.initializer;
  if (ts.isCallExpression(node) && node.expression.getText(parsed) === "submitComposerDraft") clearCallback = node.arguments[1];
  ts.forEachChild(node, visit);
}
visit(parsed);
function callback(expression: ts.Expression | undefined, bindings: Record<string, unknown>): () => void {
  if (!expression) throw new Error("Composer callback was not found");
  const js = ts.transpileModule(`const callback = ${expression.getText(parsed)};`, { compilerOptions: { target: ts.ScriptTarget.ES2021 } }).outputText;
  return new Function(...Object.keys(bindings), `${js}\nreturn callback;`)(...Object.values(bindings)) as () => void;
}
for (const nextText of ["", "original request", "new draft"]) {
  const submittedDraft = { ...baseDraft, text: "original request" };
  const draftSnapshotRef = { current: { ...submittedDraft, text: nextText } };
  const cancelledSubmissionRef = { current: false };
  let cleared = 0;
  let stopCalls = 0;
  let release!: () => void;
  const clear = callback(clearCallback, {
    cancelledSubmissionRef, submittedDraft, draftSnapshotRef, sameComposerDraft,
    setText: () => { cleared++; }, clearAttachments: () => { cleared++; },
    setWorkspaceRefs: () => { cleared++; }, setSessionRefs: () => { cleared++; },
    clearComposerDraft: () => { cleared++; }, tabId: baseDraft.tabId,
  });
  const pending = submitComposerDraft(() => new Promise<void>((resolve) => { release = resolve; }), clear, () => { throw new Error("unexpected rejection"); });
  const cancel = callback(cancelCallback, {
    submittingRef: { current: true }, cancelledSubmissionRef, draftSnapshotRef,
    onCancel: () => { stopCalls++; return submittedDraft.text; },
    setTextCaretEnd: (text: string) => { draftSnapshotRef.current = { ...draftSnapshotRef.current, text }; },
  });
  cancel();
  check(stopCalls === 1, `Stop cancels with draft ${JSON.stringify(nextText)}`);
  check(draftSnapshotRef.current.text === (nextText || submittedDraft.text), "cancel restores only an empty draft and preserves existing text");
  release();
  await pending;
  check(cleared === 0, "late successful Submit cannot clear cancelled text, attachments, references, or persisted draft");
  cancelledSubmissionRef.current = false;
  draftSnapshotRef.current = submittedDraft;
  clear();
  check(cleared === 5, "a subsequent uncancelled matching submission can still clear its draft");
}
check(composerSource.includes("submittingRef.current = true;\n    cancelledSubmissionRef.current = false;"), "each new submission resets its cancellation marker");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
