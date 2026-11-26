import assert from "node:assert/strict";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { BackgroundJobsPanel, OverviewDock } from "../components/OverviewDock";
import { LocaleProvider } from "../lib/i18n";

// The right pane is a stack: every section keeps a header row while folded, and
// only the active one renders its body. These checks pin that contract so a
// future tab-strip refactor cannot silently drop a section again.
const sections = [
  { key: "context", label: "概览", icon: null, summary: "40%", content: <div id="ctx">context body</div> },
  { key: "files", label: "文件", icon: null, content: <div id="files">files body</div> },
  { key: "background", label: "后台任务", icon: null, badge: 2, content: <div id="bg">jobs body</div> },
];

const html = renderToStaticMarkup(<OverviewDock sections={sections} active="files" onSelect={() => undefined} />);

for (const label of ["概览", "文件", "后台任务"]) {
  assert.ok(html.includes(label), `missing section header: ${label}`);
}
assert.ok(html.includes('id="files"'), "the active section must render its body");
assert.ok(!html.includes('id="ctx"'), "a folded section must not render its body");
assert.ok(!html.includes('id="bg"'), "a folded section must not render its body");
assert.ok(html.includes(">2<"), "a section badge renders its count");
assert.ok(html.includes("40%"), "a folded section still shows its summary");
assert.equal(html.split('aria-expanded="true"').length - 1, 1, "exactly one section is expanded");

const emptyJobs = renderToStaticMarkup(<LocaleProvider><BackgroundJobsPanel jobs={[]} /></LocaleProvider>);
assert.ok(emptyJobs.includes("background-jobs-panel__empty"), "no jobs renders the empty state");

const runningJobs = renderToStaticMarkup(
  <LocaleProvider><BackgroundJobsPanel jobs={[{ id: "job-1", kind: "bash", label: "npm run dev", status: "running", startedAt: Date.now() - 5000 }]} /></LocaleProvider>,
);
assert.ok(runningJobs.includes("npm run dev"), "a running job is listed by its label");
assert.ok(runningJobs.includes("bash"), "a running job shows its kind");
assert.ok(!runningJobs.includes("background-jobs-panel__empty"), "a listed job replaces the empty state");

console.log("overview-dock: ok");
