import type { DictKey, Translator } from "./i18n";

const notices: Readonly<Record<string, DictKey>> = {
  "Checking unfinished tasks and verification results before finishing.": "notice.readinessChecking",
  "The task list still has unfinished items. They have not been marked complete.": "notice.readinessPending",
  "Some results remain unverified. Review the tool results before relying on this response.": "notice.readinessUnverified",
  "A tool action is still failing after a follow-up check. Review the failed tool result before continuing; completed work and the task list have been kept.": "notice.readinessFailed",
};

export function readinessNoticeText(text: string, t: Translator): string {
  const key = Object.prototype.hasOwnProperty.call(notices, text) ? notices[text] : undefined;
  if (key) return t(key);
  // Render older notices without changing stored messages or their outcome.
  if (text.startsWith("final-answer readiness found a new failed action after targeted verification:")) {
    return t("notice.readinessLegacyStopped");
  }
  if (text.startsWith("final-answer readiness blocked:")) return t("notice.readinessChecking");
  if (text.startsWith("final-answer readiness accepted after one targeted verification attempt:")) {
    return t("notice.readinessUnverified");
  }
  return text;
}
