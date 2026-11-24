import assert from "node:assert/strict";
import { NO_BETA, readBetaCapabilities, setWebBetaBounds, hideWebBeta, exactCnyAmount, accountErrorKey, webErrorKey, receiptCost, receiptWarningKey, retainedTurnCost, retainedSettledSubtotal, type OrcaTurnSettlement, type WebBetaBounds } from "../lib/betaBridge";

const fakeWindow: { go?: unknown } = {};
Object.defineProperty(globalThis, "window", { value: fakeWindow, configurable: true });
assert.deepEqual(await readBetaCapabilities(), NO_BETA);
fakeWindow.go = { main: { App: {} } };
assert.deepEqual(await readBetaCapabilities(), NO_BETA);
fakeWindow.go = { main: { App: { BetaCapabilities: async () => ({ enabled: false, account: true, web: true, browser: true }) } } };
assert.deepEqual(await readBetaCapabilities(), NO_BETA);
fakeWindow.go = { main: { App: { BetaCapabilities: async () => ({ enabled: true, account: "true", web: true, browser: false }) } } };
assert.deepEqual(await readBetaCapabilities(), { enabled: true, account: false, web: true, browser: false });
fakeWindow.go = { main: { App: { BetaCapabilities: async () => { throw new Error("unavailable"); } } } };
assert.deepEqual(await readBetaCapabilities(), NO_BETA);

const updates: WebBetaBounds[] = [];
let inflight = 0;
let maxInflight = 0;
fakeWindow.go = { main: { App: { WebBetaBounds: async (bounds: WebBetaBounds) => {
  inflight++; maxInflight = Math.max(maxInflight, inflight);
  await new Promise(resolve => setTimeout(resolve, bounds.visible ? 25 : 0));
  updates.push(bounds); inflight--;
  if (bounds.x === -1) throw new Error("synthetic failure");
} } } };
const shown = { x: 10, y: 20, width: 600, height: 400, visible: true };
await Promise.all([setWebBetaBounds(shown), hideWebBeta()]);
assert.equal(maxInflight, 1);
assert.equal(updates[0].visible, true);
assert.equal(updates[1].visible, false);
await assert.rejects(setWebBetaBounds({ ...shown, x: -1 }));
await hideWebBeta();
assert.equal(updates[updates.length - 1].visible, false);
assert.equal(updates[updates.length - 1].width, 0);
console.log("beta bridge: capability fail-closed and serialized hide/error recovery passed");

for (const amount of ["0.000000", "0", "-9223372036854.775808", "9223372036854.775807", "900719925474099312345678.123456"]) assert.equal(exactCnyAmount(amount, "CNY"), amount);
for (const amount of ["", null, undefined, 0, 42.5, "NaN", "1e4", "1.1234567", "01.00", "<script>"]) assert.equal(exactCnyAmount(amount, "CNY"), null);
assert.equal(exactCnyAmount("0.000000", "USD"), null);
assert.equal(accountErrorKey("orca account: authorization rejected; access_token=PRIVATE"), "beta.error.authorization");
assert.equal(accountErrorKey(new Error("orca account: system browser unavailable")), "beta.error.browser");
assert.equal(accountErrorKey(new Error("orca account: desktop service not deployed")), "beta.error.serviceUnavailable");
assert.equal(accountErrorKey("PRIVATE_TOKEN_AND_CONFIGURATION"), "beta.requestFailed");
console.log("beta account: exact large money strings, unknown amounts and public-only error categories passed");
assert.equal(webErrorKey(new Error("web_recheck_timeout\ncontext deadline exceeded")), "beta.webError.timeout");
assert.equal(webErrorKey("web_recheck_adapter"), "beta.webError.adapter");
assert.equal(webErrorKey("web_recheck_signed_out"), "beta.webError.signedOut");
for (const error of ["PRIVATE_TOKEN", "web_recheck_timeout PRIVATE_TOKEN", "toString", "__proto__", {}, null]) assert.equal(webErrorKey(error), "beta.requestFailed");
console.log("beta web: bounded error categories without transport secrets passed");

const receipt = { status: "settled", cost: "900719925474099312345678.123456", currency: "CNY" };
for (const status of ["settled", "estimated"]) assert.equal(receiptCost({ ...receipt, status }), receipt.cost);
for (const status of ["pending", "unknown", "unrecognized", ""]) {
  for (const cost of [receipt.cost, "42.000001", "0", "PRIVATE_MALFORMED_COST"]) assert.equal(receiptCost({ ...receipt, status, cost }), null);
}
assert.equal(receiptCost({ ...receipt, cost: "" }), null);
assert.equal(receiptCost({ ...receipt, cost: "NaN" }), null);
assert.equal(receiptCost({ ...receipt, currency: "USD" }), null);
for (const error of [undefined, null, "", "  ", false, 0]) assert.equal(receiptWarningKey(error), null);
for (const error of ["receipt journal unavailable", "session expired PRIVATE_TOKEN", "C:/PRIVATE_CONFIG_PATH"]) assert.equal(receiptWarningKey(error), "beta.receiptStorageWarning");
assert.equal(receiptWarningKey(""), null);
console.log("beta receipts: status-gated costs and fixed storage warning/clear category passed");

const turn: OrcaTurnSettlement = { turnId: "synthetic-turn", scope: "retained_requests", status: "settled", currency: "CNY", cost: "9007199254.740994", settledSubtotal: "9007199254.740994", requests: 2, settledRequests: 2 };
assert.equal(retainedTurnCost(turn), "9007199254.740994");
assert.equal(retainedTurnCost({ ...turn, status: "estimated" }), "9007199254.740994");
for (const status of ["pending", "unknown", "unrecognized"]) assert.equal(retainedTurnCost({ ...turn, status }), null);
assert.equal(retainedTurnCost({ ...turn, cost: "" }), null);
assert.equal(retainedTurnCost({ ...turn, currency: "USD" }), null);
assert.equal(retainedTurnCost({ ...turn, scope: "historical_bill" }), null);
assert.equal(retainedSettledSubtotal({ ...turn, status: "pending", cost: "", settledSubtotal: "0.000001" }), "0.000001");
assert.equal(retainedSettledSubtotal({ ...turn, status: "unknown", settledSubtotal: "" }), null);
assert.equal(retainedSettledSubtotal({ ...turn, scope: "historical_bill" }), null);
console.log("beta turn summaries: retained scope, exact costs, pending blanks and independent settled subtotal passed");
