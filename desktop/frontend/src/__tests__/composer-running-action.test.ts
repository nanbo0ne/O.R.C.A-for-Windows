import { composerDraftState } from "../lib/composerRunningAction";
import { readFileSync } from "node:fs";

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

function state(overrides: Partial<Parameters<typeof composerDraftState>[0]> = {}) {
  return composerDraftState({
    text: "",
    attachmentCount: 0,
    workspaceReferenceCount: 0,
    sessionReferenceCount: 0,
    pendingPasteCount: 0,
    ...overrides,
  });
}

console.log("\ncomposer running action");
check(!state().hasDraftContent, "empty composer keeps the stop action");
check(!state({ text: "   \n" }).hasDraftContent, "whitespace does not replace stop with send");
check(state({ text: "next request" }).hasSendableContent, "text enables queued send");
check(state({ attachmentCount: 1 }).hasSendableContent, "attachments enable queued send");
check(state({ workspaceReferenceCount: 1 }).hasSendableContent, "workspace references enable queued send");
check(state({ sessionReferenceCount: 1 }).hasSendableContent, "session references enable queued send");
const pendingPaste = state({ pendingPasteCount: 1 });
check(pendingPaste.hasDraftContent && !pendingPaste.hasSendableContent, "pending paste counts as draft content but remains unsendable");
const composer = readFileSync(new URL("../components/Composer.tsx", import.meta.url), "utf8");
const primaryStart = composer.indexOf('className={`composer-runstatus__primary ');
const primary = composer.slice(primaryStart, composer.indexOf("</button>", primaryStart));
check(primaryStart >= 0 && primary.includes("onClick={handleCancel}") && primary.includes("composer-runstatus__primary--stop") && !primary.includes("submit("), "mouse primary stays Stop regardless of new draft content");
check(primary.includes("disabled={cancelRequested && !cancelSlow}") && !primary.includes("submitting"), "pending Submit does not disable Stop and slow cancellation allows retry");
check(composer.includes('if (e.key === "Enter" && !e.shiftKey && !composing)') && composer.includes("void submit(Boolean(running && (e.ctrlKey || e.metaKey)));"), "Enter queues a draft and Ctrl/Cmd+Enter still guides while running");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
