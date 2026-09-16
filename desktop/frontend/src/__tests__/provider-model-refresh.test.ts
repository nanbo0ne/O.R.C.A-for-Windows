// Run: tsx src/__tests__/provider-model-refresh.test.ts

import { isLikelyChatModel, mergedFetchedProviderModels, providerDefaultModel, providerModelCandidates } from "../lib/providerModels";
import { app } from "../lib/bridge";
import { canonicalModelRef, DEEPSEEK_FLASH_REF, DEEPSEEK_PRO_REF, isOfficialDeepSeekProvider, modelDisplayLabel, OFFICIAL_DEEPSEEK_MODELS, providerModelIDs, providerModelLabel, providerModelRef, selectableModels } from "../lib/modelCatalog";
import type { ModelInfo } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nprovider model refresh");

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"]),
  ["coding-pro", "chat", "vision"],
  "appends discovered models without removing curated ones",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh preserves manually curated model list",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh does not re-add deleted models",
);

eq(
  mergedFetchedProviderModels(["mimo-v2.5-pro"], ["mimo-v2-flash", "mimo-v2-omni", "mimo-v2.5-pro"], { preserveCurated: true }),
  ["mimo-v2.5-pro"],
  "manual access refresh preserves selected MiMo model instead of importing provider catalog",
);

eq(
  providerModelCandidates(["mimo-v2.5-pro"], ["mimo-v2-flash", "mimo-v2-omni", "mimo-v2.5-pro"]),
  ["mimo-v2.5-pro", "mimo-v2-flash", "mimo-v2-omni"],
  "manual access refresh can show provider catalog as unsaved candidates",
);

eq(
  providerModelCandidates(["mimo-v2.5-pro"], ["mimo-v2.5-asr", "mimo-v2.5-tts", "mimo-v2.5", "mimo-v2.5-pro"]),
  ["mimo-v2.5-pro", "mimo-v2.5"],
  "manual access refresh filters non-chat candidates before saving",
);

eq(
  [
    isLikelyChatModel("mimo-v2.5-pro"),
    isLikelyChatModel("mimo-v2.5-asr"),
    isLikelyChatModel("mimo-v2.5-tts"),
    isLikelyChatModel("text-embedding-3-small"),
  ],
  [true, false, false, false],
  "matches backend non-chat model heuristic",
);

eq(
  mergedFetchedProviderModels([], ["coding-pro", "chat"], { preserveCurated: true }),
  ["coding-pro", "chat"],
  "background refresh can populate an empty model list",
);

eq(
  providerDefaultModel("coding-pro", ["coding-pro", "chat"]),
  "coding-pro",
  "preserves current default when it remains available",
);

eq(
  providerDefaultModel("deleted", ["coding-pro", "chat"]),
  "coding-pro",
  "falls back to first saved model when default is unavailable",
);

const oldFlash = "deepseek/deepseek-v4-flash";
const oldVision = "deepseek/deepseek-v4-flash-vision-exp";
const refs = [oldFlash, oldVision, DEEPSEEK_FLASH_REF, DEEPSEEK_PRO_REF, "proxy/deepseek-v4-flash", "proxy/deepseek-flash"];
const catalog: ModelInfo[] = refs.map((ref) => ({ ...OFFICIAL_DEEPSEEK_MODELS.find((model) => model.ref === canonicalModelRef(ref)), ref, provider: ref.split("/")[0], model: ref.split("/")[1], current: ref === oldVision, metadataSource: ref.startsWith("deepseek/") ? "deepseek_official" : "discovery" }));
const selectable = selectableModels(catalog);
eq(selectable.map((model) => model.ref), [DEEPSEEK_FLASH_REF, DEEPSEEK_PRO_REF, "proxy/deepseek-v4-flash", "proxy/deepseek-flash"], "official aliases collapse without removing custom provider models");
eq(selectable.map((model) => model.current), [true, false, false, false], "a legacy current model marks its canonical option selected");
eq(selectableModels([...catalog].reverse()).find((model) => model.ref === DEEPSEEK_FLASH_REF)?.current, true, "alias selection survives catalog order changes");
eq(selectable.slice(0, 2).map((model) => [model.model, model.vision, model.contextWindow, model.toolUse]), [["deepseek-flash", "supported", 1_000_000, "supported"], ["deepseek-v4-pro", "unsupported", 1_000_000, "supported"]], "Flash has native vision, 1M context and tools; text-only Pro stays selectable");
eq(refs.map((ref) => modelDisplayLabel(ref, undefined, true)), ["DeepSeek V4.1 Flash", "DeepSeek V4.1 Flash", "DeepSeek V4.1 Flash", "DeepSeek V4 Pro", "deepseek-v4-flash", "deepseek-flash"], "labels require the complete official ref and confirmed identity");
eq(modelDisplayLabel(DEEPSEEK_FLASH_REF, "old label", true), "DeepSeek V4.1 Flash", "confirmed official label wins over a stale backend display label");
eq(modelDisplayLabel("proxy/deepseek-flash", "My Flash"), "My Flash", "custom display labels are retained");
eq(["deepseek-flash", "deepseek-v4-flash", "deepseek/deepseek-v4-flash-extra", "proxy/deepseek-v4-flash-vision-exp"].map(canonicalModelRef), ["deepseek-flash", "deepseek-v4-flash", "deepseek/deepseek-v4-flash-extra", "proxy/deepseek-v4-flash-vision-exp"], "bare IDs, similar names and custom refs are not official aliases");
const officialProvider = { name: "deepseek", builtIn: true, kind: "openai", baseUrl: "https://api.deepseek.com" };
eq(providerModelIDs(officialProvider, ["deepseek-v4-flash", "deepseek-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"]), ["deepseek-flash", "deepseek-v4-pro"], "settings and discovery expose two canonical official choices");
eq(providerModelIDs({ ...officialProvider, name: "proxy" }, ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"]), ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"], "settings discovery preserves custom aliases");

const manualModel: ModelInfo = { ref: DEEPSEEK_FLASH_REF, provider: "deepseek", model: "deepseek-flash", current: true,
  metadataSource: "deepseek_official", vision: "unsupported", contextWindow: 32000, contextWindowConfirmed: true,
  contextSource: "user", toolUse: "unsupported", structuredOutput: "unknown", pricingAvailable: false, currency: "" };
eq(selectableModels([manualModel]), [manualModel], "manual unsupported vision, lower context and every backend capability survive unchanged");
const unknownModel: ModelInfo = { ref: DEEPSEEK_FLASH_REF, provider: "deepseek", model: "deepseek-flash", current: true };
eq(selectableModels([unknownModel]), [unknownModel], "missing backend metadata is not replaced by official capabilities");
const customNamedDeepSeek = refs.slice(0, 4).map((ref) => ({ ...manualModel, ref, model: ref.split("/")[1], metadataSource: "discovery" }));
eq(selectableModels(customNamedDeepSeek), customNamedDeepSeek, "custom endpoint named deepseek keeps every model and backend field");
eq(customNamedDeepSeek.map((model) => modelDisplayLabel(model.ref, model.model, model.metadataSource === "deepseek_official")), customNamedDeepSeek.map((model) => model.model), "custom endpoint named deepseek is never given official branding");
eq(modelDisplayLabel(DEEPSEEK_FLASH_REF, "Relay label"), "Relay label", "a full ref alone cannot establish official identity");
eq(selectableModels([{ ...manualModel, ref: oldVision, model: "deepseek-v4-flash-vision-exp" }, { ...manualModel, current: false, vision: "supported", contextWindow: 1_000_000 }])[0], manualModel, "current alias retains its manual restrictions when deduplicated");
for (const baseUrl of ["https://relay.example/v1", "http://api.deepseek.com", "https://api.deepseek.com.evil.example", "https://api.deepseek.com:8443", "https://api.deepseek.com/proxy", "https://user@api.deepseek.com", "https://api.deepseek.com?relay=1", "https://api.deepseek.com#relay", "not-a-url"]) {
  const provider = { ...officialProvider, baseUrl };
  eq(isOfficialDeepSeekProvider(provider), false, `rejects non-official endpoint ${baseUrl}`);
  eq(providerModelIDs(provider, ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"]), ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"], `custom aliases survive ${baseUrl}`);
  eq(providerModelRef(oldVision, provider), oldVision, `saved custom ref survives ${baseUrl}`);
  eq(providerModelLabel(DEEPSEEK_FLASH_REF, provider), "deepseek-flash", `no official label for ${baseUrl}`);
}
eq(["https://api.deepseek.com", "https://api.deepseek.com/", "https://api.deepseek.com/v1", "https://api.deepseek.com/v1/"].map((baseUrl) => isOfficialDeepSeekProvider({ ...officialProvider, baseUrl })), [true, true, true, true], "only supported official endpoint forms are accepted");
eq([isOfficialDeepSeekProvider({ ...officialProvider, builtIn: false }), isOfficialDeepSeekProvider({ ...officialProvider, name: "custom" }), isOfficialDeepSeekProvider({ ...officialProvider, kind: "anthropic" }), isOfficialDeepSeekProvider(undefined)], [false, false, false, false], "provider identity and protocol are required even on the official host");
for (const name of ["deepseek-flash", "deepseek-pro"]) {
  const provider = { ...officialProvider, name };
  const ref = `${name}/deepseek-flash`;
  const legacyRef = `${name}/deepseek-v4-flash-vision-exp`;
  const liveModel = { ...manualModel, ref, provider: name };
  eq(modelDisplayLabel(ref, "deepseek-flash", liveModel.metadataSource === "deepseek_official"), "DeepSeek V4.1 Flash", `${name} resolved official Flash label uses backend identity`);
  eq(providerModelLabel(`${name}/deepseek-v4-pro`, provider), "DeepSeek V4 Pro", `${name} settings labels Pro by model ID`);
  eq(providerModelRef(legacyRef, provider), ref, `${name} normalization retains provider and credential slot`);
  eq(providerModelIDs(provider, ["deepseek-v4-flash", "deepseek-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"]), ["deepseek-flash", "deepseek-v4-pro"], `${name} official aliases do not add selectable models`);
  eq(selectableModels([{ ...liveModel, ref: legacyRef, model: "deepseek-v4-flash-vision-exp" }])[0], liveModel, `${name} alias keeps manual capabilities and provider identity`);
  eq(selectableModels([manualModel, liveModel]).map(model => model.ref), [DEEPSEEK_FLASH_REF, ref], `${name} distinct key slot is not deduplicated with deepseek`);
  const relay = { ...provider, baseUrl: "https://relay.example/v1" };
  eq(providerModelRef(legacyRef, relay), legacyRef, `${name} project relay keeps its original ref`);
  eq(providerModelLabel(ref, relay), "deepseek-flash", `${name} project relay has no official branding`);
  eq(selectableModels([{ ...liveModel, ref: legacyRef, model: "deepseek-v4-flash-vision-exp", metadataSource: "discovery" }])[0].ref, legacyRef, `${name} backend custom identity preserves legacy model`);
}

const initialSettings = await app.Settings();
const tabs = await app.ListTabs();
const activeTab = tabs.find((tab) => tab.active)!;
eq((await app.ModelsForTab(activeTab.id)).map((model) => model.ref), [DEEPSEEK_FLASH_REF, DEEPSEEK_PRO_REF], "mock picker exposes exactly the two official models");
eq(initialSettings.providers[0].models, ["deepseek-flash", "deepseek-v4-pro"], "mock settings uses canonical API IDs");
eq(initialSettings.effectiveVisionModel, DEEPSEEK_FLASH_REF, "automatic vision uses native Flash");
eq(await app.EffortForTab(activeTab.id), { supported: true, current: "auto", default: "high", levels: ["auto", "low", "high", "max"] }, "mock effort offers auto, low, high and max with auto defaulting to high");
await app.SetEffortForTab(activeTab.id, "low");
eq((await app.EffortForTab(activeTab.id)).current, "low", "low effort can be selected");
for (const ref of [DEEPSEEK_PRO_REF, oldVision, DEEPSEEK_FLASH_REF]) {
  await app.SetModelForTab(activeTab.id, ref);
  eq((await app.ModelsForTab(activeTab.id)).filter((model) => model.current).map((model) => model.ref), [canonicalModelRef(ref)], `mock switching selects ${ref}`);
  eq((await app.MetaForTab(activeTab.id)).label, modelDisplayLabel(ref, undefined, true), `status label follows ${ref}`);
  eq((await app.ContextUsageForTab(activeTab.id)).modelRef, canonicalModelRef(ref), `status carries the exact ref for ${ref}`);
}
eq((await app.ContextPanel(activeTab.id)).windowTokens, 1_000_000, "context panel agrees with the 1M catalog");
eq((await app.ProbeModelVision(DEEPSEEK_FLASH_REF)).status, "supported", "mock recognizes native Flash vision");
eq((await app.ProbeModelVision(DEEPSEEK_PRO_REF)).status, "unsupported", "mock keeps Pro text only");
eq((await app.ProbeModelVision("proxy/deepseek-flash")).status, "unknown", "mock does not assign official vision to a custom model");
await app.SaveProvider({ ...initialSettings.providers[0], name: "proxy", builtIn: false, models: ["deepseek-flash", "deepseek-v4-flash-vision-exp"], default: "deepseek-flash" });
eq((await app.Models()).filter((model) => model.provider === "proxy").map((model) => model.ref), ["proxy/deepseek-flash", "proxy/deepseek-v4-flash-vision-exp"], "mock preserves configured custom models");
await app.SetModelForTab(activeTab.id, "proxy/deepseek-flash");
eq((await app.MetaForTab(activeTab.id)).label, "deepseek-flash", "custom status labels do not borrow official branding");
eq((await app.GetLocalAICatalog()).supported, false, "catalog changes keep local AI disabled");
eq((await app.GetComputerUseState()).capabilities.temporarilyDisabled, true, "catalog changes keep computer use disabled");
eq([(await app.Settings()).processDisplayMode, (await app.Settings()).expandThinking], [initialSettings.processDisplayMode, initialSettings.expandThinking], "model and effort changes preserve reasoning visibility preferences");
await app.DeleteProvider("proxy");
await app.SaveProvider({ ...initialSettings.providers[0], baseUrl: "https://relay.example/v1", models: ["deepseek-v4-flash", "deepseek-flash", "deepseek-v4-flash-vision-exp"], contextWindow: 16000 });
await app.SetModelForTab(activeTab.id, oldVision);
const relayModels = await app.ModelsForTab(activeTab.id);
eq(selectableModels(relayModels).map((model) => model.ref), [oldFlash, DEEPSEEK_FLASH_REF, oldVision], "same-name relay mock retains its full selectable model list");
eq((await app.MetaForTab(activeTab.id)).label, "deepseek-v4-flash-vision-exp", "same-name relay status label stays custom");
eq((await app.ContextUsageForTab(activeTab.id)).window, 16000, "same-name relay retains its smaller context");
eq((await app.ProbeModelVision(DEEPSEEK_FLASH_REF)).status, "unknown", "same-name relay does not borrow official native vision");
await app.SaveProvider(initialSettings.providers[0]);
await app.SetModelForTab(activeTab.id, DEEPSEEK_FLASH_REF);
await app.SetEffortForTab(activeTab.id, "auto");
for (const name of ["deepseek-flash", "deepseek-pro"]) {
  await app.SaveProvider({ ...initialSettings.providers[0], name, apiKeyEnv: `${name}_FIXTURE_KEY`, models: ["deepseek-flash", "deepseek-v4-pro"] });
  await app.SetModelForTab(activeTab.id, `${name}/deepseek-flash`);
  eq((await app.ModelsForTab(activeTab.id)).filter(model => model.current).map(model => model.ref), [`${name}/deepseek-flash`], `${name} mock retains its distinct selected provider`);
  eq((await app.MetaForTab(activeTab.id)).label, "DeepSeek V4.1 Flash", `${name} mock status shows resolved Flash label`);
  await app.DeleteProvider(name);
}
await app.SetModelForTab(activeTab.id, DEEPSEEK_FLASH_REF);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
