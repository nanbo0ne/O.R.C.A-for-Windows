import assert from "node:assert/strict";
import { test } from "node:test";
import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EffortSwitcher } from "../components/EffortSwitcher";
import { LocaleProvider } from "../lib/i18n";
import { en } from "../locales/en";
import { zh } from "../locales/zh";

type Props = ComponentProps<typeof EffortSwitcher>;
const effort = { supported: true, current: "auto", default: "high", levels: ["auto", "low", "high"] };

function render(overrides: Partial<Props> = {}, language = "en") {
  const navigatorDescriptor = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { language } });
  try {
    return renderToStaticMarkup(createElement(LocaleProvider, null,
      createElement(EffortSwitcher, { effort, disabled: false, onPick: () => {}, ...overrides })));
  } finally {
    if (navigatorDescriptor) Object.defineProperty(globalThis, "navigator", navigatorDescriptor);
    else Reflect.deleteProperty(globalThis, "navigator");
  }
}

for (const disabled of [false, true]) {
  for (const saving of [false, true]) {
    for (const running of [false, true]) {
      test(`lock state: disabled=${disabled}, saving=${saving}, running=${running}`, () => {
        const html = render({ disabled, saving, running });
        const trigger = html.match(/<button\b[^>]*>/)?.[0];
        assert.ok(trigger);
        assert.equal(trigger.includes('disabled=""'), disabled || saving || running);
        assert.ok(trigger.includes(`aria-busy="${saving}"`));
        assert.ok(trigger.includes('aria-expanded="false"'));
        if (saving) {
          assert.ok(trigger.includes(`aria-description="${en["status.effortSaving"]}"`));
          assert.ok(html.includes(`>${en["status.effortSaving"]}</span>`));
        } else if (running) {
          assert.ok(trigger.includes(`aria-description="${en["status.effortRunningHint"]}"`));
        } else {
          assert.ok(!trigger.includes("aria-description"));
        }
      });
    }
  }
}

test("auto explains a concrete effective default", () => {
  const html = render();
  assert.ok(html.includes(en["status.effortAutoTitle"].replace("{def}", "high")));
  assert.ok(!html.includes(en["status.effortAutoOmittedTitle"]));
});

for (const defaultValue of ["auto", ""]) {
  test(`auto explains omitted effort when default is ${JSON.stringify(defaultValue)}`, () => {
    const html = render({ effort: { ...effort, default: defaultValue } });
    assert.ok(html.includes(en["status.effortAutoOmittedTitle"]));
    assert.ok(!html.includes("effective default:"));
  });
}

test("explicit effort retains its label while running", () => {
  const html = render({ effort: { ...effort, current: "low" }, running: true });
  assert.ok(html.includes(">low</span>"));
  assert.ok(!html.includes("effective default:"));
  assert.ok(!html.includes("effort parameter omitted"));
});

test("unsupported effort remains hidden unless requested", () => {
  assert.equal(render({ effort: undefined, saving: true }), "");
  const html = render({ effort: undefined, showDefault: true, running: true });
  assert.ok(html.includes('disabled=""'));
  assert.ok(html.includes(en["status.effortModelDefaultHint"]));
  assert.ok(html.includes(`>${en["status.effortModelDefault"]}</span>`));
});

test("Chinese busy and auto hints are localized", () => {
  const saving = render({ saving: true }, "zh-CN");
  assert.ok(saving.includes(zh["status.effortSaving"]));
  assert.ok(saving.includes(zh["status.effortAutoTitle"].replace("{def}", "high")));
  const running = render({ running: true, effort: { ...effort, default: "auto" } }, "zh-CN");
  assert.ok(running.includes(zh["status.effortRunningHint"]));
  assert.ok(running.includes(zh["status.effortAutoOmittedTitle"]));
});

test("sync and async parent callbacks are accepted without invoking them on render", () => {
  let calls = 0;
  render({ onPick: () => { calls += 1; } });
  render({ onPick: async () => { calls += 1; } });
  assert.equal(calls, 0);
});
