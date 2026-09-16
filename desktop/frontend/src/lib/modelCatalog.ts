import type { ModelInfo, ProviderView } from "./types";

export const DEEPSEEK_FLASH_REF = "deepseek/deepseek-flash";
export const DEEPSEEK_PRO_REF = "deepseek/deepseek-v4-pro";
export const DEEPSEEK_EFFORT_LEVELS = ["auto", "low", "high", "max"];
export const DEEPSEEK_DEFAULT_EFFORT = "high";
const OFFICIAL_PROVIDER_NAMES = ["deepseek", "deepseek-flash", "deepseek-pro"];

export const OFFICIAL_DEEPSEEK_MODELS = [
  { ref: DEEPSEEK_FLASH_REF, provider: "deepseek", model: "deepseek-flash", displayName: "DeepSeek V4.1 Flash", contextWindow: 1_000_000, contextWindowConfirmed: true, vision: "supported", toolUse: "supported" },
  { ref: DEEPSEEK_PRO_REF, provider: "deepseek", model: "deepseek-v4-pro", displayName: "DeepSeek V4 Pro", contextWindow: 1_000_000, contextWindowConfirmed: true, vision: "unsupported", toolUse: "supported" },
] as const;

// Only full official refs are aliases. Identical API IDs on custom providers stay intact.
export function canonicalModelRef(ref: string): string {
  const [provider, model] = ref.split("/");
  if (!OFFICIAL_PROVIDER_NAMES.includes(provider) || ref !== `${provider}/${model}`) return ref;
  switch (model) {
    case "deepseek-v4-flash":
    case "deepseek-v4-flash-vision-exp":
      return `${provider}/deepseek-flash`;
    default:
      return ref;
  }
}

export function officialModelInfo(ref: string) {
  const canonicalRef = canonicalModelRef(ref);
  const [provider, modelID] = canonicalRef.split("/");
  if (!OFFICIAL_PROVIDER_NAMES.includes(provider) || canonicalRef !== `${provider}/${modelID}`) return undefined;
  const model = OFFICIAL_DEEPSEEK_MODELS.find((item) => item.model === modelID);
  return model ? { ...model, ref: canonicalRef, provider } : undefined;
}

export function modelDisplayLabel(ref: string, fallback?: string, officialIdentity = false): string {
  const official = officialIdentity ? officialModelInfo(ref) : undefined;
  if (official) return official.displayName;
  return fallback?.trim() || ref.slice(ref.indexOf("/") + 1);
}

type ProviderIdentity = Pick<ProviderView, "name" | "kind" | "baseUrl" | "builtIn">;

export function isOfficialDeepSeekProvider(provider?: ProviderIdentity): boolean {
  if (!provider?.builtIn || !OFFICIAL_PROVIDER_NAMES.includes(provider.name) || (provider.kind && provider.kind !== "openai")) return false;
  try {
    const url = new URL(provider.baseUrl.trim());
    return url.protocol === "https:" && url.host === "api.deepseek.com" && !url.username && !url.password
      && !url.search && !url.hash && ["", "/v1"].includes(url.pathname.replace(/\/+$/, ""));
  } catch {
    return false;
  }
}

export function providerModelRef(ref: string, provider?: ProviderIdentity): string {
  return isOfficialDeepSeekProvider(provider) && ref.startsWith(`${provider!.name}/`) ? canonicalModelRef(ref) : ref;
}

export function providerModelLabel(ref: string, provider?: ProviderIdentity, fallback?: string): string {
  return modelDisplayLabel(ref, fallback, isOfficialDeepSeekProvider(provider) && ref.startsWith(`${provider!.name}/`));
}

export function providerModelIDs(provider: ProviderIdentity, models: string[]): string[] {
  return [...new Set(models.map((model) => providerModelRef(`${provider.name}/${model}`, provider)
    .slice(provider.name.length + 1)))];
}

export function selectableModels(models: ModelInfo[]): ModelInfo[] {
  const result = new Map<string, ModelInfo>();
  for (const model of models) {
    const ref = model.metadataSource === "deepseek_official" ? canonicalModelRef(model.ref) : model.ref;
    const previous = result.get(ref);
    // Keep one complete backend record; capability overrides must never be synthesized here.
    const selected = previous && (previous.current || (!model.current && previous.ref === model.ref)) ? previous : model;
    result.set(ref, {
      ...selected,
      ref,
      model: ref === selected.ref ? selected.model : ref.slice(ref.indexOf("/") + 1),
      current: model.current || previous?.current || false,
    });
  }
  return [...result.values()];
}
