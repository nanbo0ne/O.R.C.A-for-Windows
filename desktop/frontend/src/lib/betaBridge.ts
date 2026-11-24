// Public, token-free beta contract. Keep experimental bindings out of bridge.ts.
export interface BetaCapabilities { enabled: boolean; account: boolean; web: boolean; browser: boolean }
export interface OrcaAccountStatus {
  state: "signed_out" | "restoring" | "signing_in" | "signed_in" | "logging_out" | "error";
  authenticated: boolean;
  needsSignOut?: boolean;
  displayName: string;
  error: string;
  receiptError?: string;
}
export interface OrcaAccountWallet {
  currency: "CNY";
  cash: string;
  held: string;
  available: string;
  gifts: Array<{ groups: string; available: string }>;
  error: string;
}
export interface OrcaAccountReceipt {
  requestId: string;
  turnId: string;
  purpose: string;
  status: string;
  cost: string;
  currency: string;
}
export interface OrcaTurnSettlement {
  turnId: string;
  status: string;
  currency: string;
  scope: string;
  cost: string;
  settledSubtotal: string;
  requests: number;
  settledRequests: number;
}
export interface WebBetaSettings {
  enabled: boolean;
  rpm: number;
  reservePaused: boolean;
  openingRounds: number;
  openings: string[];
  thinking: boolean;
  search: boolean;
}
export interface WebBetaStatus {
  sendAuthorized: boolean;
  settings: WebBetaSettings;
  browser: { ready: boolean; busy: boolean; blocked?: string };
  pending: number;
  ready: number;
  preparing: boolean;
  sent: number;
  error?: string;
  blocked?: string;
  requestLimit?: number;
}
export interface WebBetaBounds { x: number; y: number; width: number; height: number; visible: boolean }
export interface BetaBridge {
  BetaCapabilities(): Promise<BetaCapabilities>;
  OrcaAccountStatus(): Promise<OrcaAccountStatus>;
  OrcaAccountLogin(): Promise<void>;
  OrcaAccountCancelLogin(): Promise<void>;
  OrcaAccountLogout(): Promise<void>;
  OrcaAccountWallet(): Promise<OrcaAccountWallet>;
  OrcaAccountReceipts(): Promise<OrcaAccountReceipt[]>;
  OrcaAccountTurnSettlements(): Promise<OrcaTurnSettlement[]>;
  OrcaAccountRefreshModels(): Promise<void>;
  WebBetaOpen(): Promise<WebBetaStatus>;
  WebBetaStatus(): Promise<WebBetaStatus>;
  WebBetaConfigure(settings: WebBetaSettings): Promise<void>;
  WebBetaRecover(): Promise<void>;
  WebBetaUseCurrentLogin(): Promise<void>;
  // Bounds are physical pixels. Hiding MUST NOT stop rendering or clear the queue.
  WebBetaBounds(bounds: WebBetaBounds): Promise<void>;
  WebBetaLogout(): Promise<void>;
}

export function betaBridge(): BetaBridge {
  const binding = (window as unknown as { go?: { main?: { App?: BetaBridge } } }).go?.main?.App;
  if (!binding) throw new Error("Beta desktop bindings unavailable");
  return binding;
}

export const NO_BETA: BetaCapabilities = { enabled: false, account: false, web: false, browser: false };

export async function readBetaCapabilities(): Promise<BetaCapabilities> {
  try {
    const value = await betaBridge().BetaCapabilities();
    if (value?.enabled !== true) return NO_BETA;
    return { enabled: true, account: value.account === true, web: value.web === true, browser: value.browser === true };
  } catch {
    return NO_BETA;
  }
}

// Serialize native presentation updates so a late resize cannot overtake hide.
let boundsTail: Promise<void> = Promise.resolve();
export function setWebBetaBounds(bounds: WebBetaBounds): Promise<void> {
  boundsTail = boundsTail.catch(() => {}).then(() => betaBridge().WebBetaBounds(bounds));
  return boundsTail;
}
export function hideWebBeta(): Promise<void> {
  return setWebBetaBounds({ x: 0, y: 0, width: 0, height: 0, visible: false });
}

// Never parse server money through Number, or infer zero from an absent cost.
export function exactCnyAmount(value: unknown, currency: unknown): string | null {
  return currency === "CNY" && typeof value === "string" && /^-?(?:0|[1-9]\d*)(?:\.\d{1,6})?$/.test(value) ? value : null;
}

export function receiptCost(row: Pick<OrcaAccountReceipt, "status" | "cost" | "currency">): string | null {
  if (row.status !== "settled" && row.status !== "estimated") return null;
  return exactCnyAmount(row.cost, row.currency);
}

export function retainedTurnCost(row: OrcaTurnSettlement): string | null {
  return row.scope === "retained_requests" ? receiptCost(row) : null;
}

export function retainedSettledSubtotal(row: OrcaTurnSettlement): string | null {
  return row.scope === "retained_requests" ? exactCnyAmount(row.settledSubtotal, row.currency) : null;
}

export function receiptWarningKey(error: unknown) {
  return typeof error === "string" && error.trim() ? "beta.receiptStorageWarning" : null;
}

export function webErrorKey(error: unknown) {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const code = message.split("\n", 1)[0];
  const keys = {
    web_recheck_busy: "beta.webError.busy",
    web_recheck_timeout: "beta.webError.timeout",
    web_recheck_unavailable: "beta.webError.unavailable",
    web_recheck_identity: "beta.webError.identity",
    web_recheck_signed_out: "beta.webError.signedOut",
    web_recheck_challenge: "beta.webError.challenge",
    web_recheck_restricted: "beta.webError.restricted",
    web_recheck_rate_limited: "beta.webError.rateLimited",
    web_recheck_adapter: "beta.webError.adapter",
    web_recheck_not_ready: "beta.webError.notReady",
  } as const;
  return Object.prototype.hasOwnProperty.call(keys, code) ? keys[code as keyof typeof keys] : "beta.requestFailed";
}

// Only public error categories cross into UI copy; raw URLs, credentials and
// configuration details from transport/storage errors are never rendered.
export function accountErrorKey(error: unknown) {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if (/desktop service not deployed/i.test(message)) return "beta.error.serviceUnavailable";
  if (/sign-in required|session expired|no stored session/i.test(message)) return "beta.error.signInRequired";
  if (/authorization rejected|\bnonce\b|\bpkce\b|state mismatch/i.test(message)) return "beta.error.authorization";
  if (/secure storage/i.test(message)) return "beta.error.storage";
  if (/system browser unavailable/i.test(message)) return "beta.error.browser";
  if (/deadline exceeded|timed?\s*out|timeout/i.test(message)) return "beta.error.timeout";
  if (/request unavailable|network|offline/i.test(message)) return "beta.error.network";
  if (/invalid server response|protocol.*not supported/i.test(message)) return "beta.error.response";
  if (/authorization is already in progress/i.test(message)) return "beta.authorizing";
  if (/sign out of the current/i.test(message)) return "beta.error.signOutFirst";
  if (/account generation changed/i.test(message)) return "beta.error.changed";
  return "beta.requestFailed";
}

// Compact header view of both connections. The full panel polls at 1.5s while it
// is open; the capsule polls slowly and on focus, so a login finished in an
// external browser shows up without the user opening the panel first.
export interface BetaAccountSummary {
  orca: OrcaAccountStatus["state"] | "unknown";
  orcaName: string;
  web: "connected" | "retained" | "attention" | "idle" | "unknown";
}

export const UNKNOWN_ACCOUNT_SUMMARY: BetaAccountSummary = { orca: "unknown", orcaName: "", web: "unknown" };

// The header capsule and the open panel both need the same status. Coalesce a
// request that is already in flight instead of stacking a second one: a slow
// backend answer must not queue behind itself, and the panel's own faster poll
// keeps the value fresh once it resolves.
let accountStatusInFlight: Promise<OrcaAccountStatus> | null = null;

export function readOrcaAccountStatus(): Promise<OrcaAccountStatus> {
  if (accountStatusInFlight) return accountStatusInFlight;
  accountStatusInFlight = Promise.resolve().then(() => betaBridge().OrcaAccountStatus()).finally(() => { accountStatusInFlight = null; });
  return accountStatusInFlight;
}

// One status read at a time, shared by the header capsule and the panel. Both
// ask the same two endpoints, so overlapping requests would only queue work the
// backend already answers once, and a slow reply would stack up behind itself.
let summaryTail: Promise<BetaAccountSummary> = Promise.resolve(UNKNOWN_ACCOUNT_SUMMARY);

export function readBetaAccountSummary(capabilities: BetaCapabilities): Promise<BetaAccountSummary> {
  const run = async (): Promise<BetaAccountSummary> => {
    const [account, web] = await Promise.all([
      capabilities.account
        ? readOrcaAccountStatus().then(value => ({ state: value?.state, name: value?.displayName || "" })).catch(() => null)
        : Promise.resolve(null),
      capabilities.web
        ? betaBridge().WebBetaStatus().then(value => {
            if (!value || value.error || value.blocked || value.browser?.blocked) return "attention" as const;
            if (value.browser?.ready || value.browser?.busy) return value.settings?.enabled ? "connected" as const : "retained" as const;
            return "idle" as const;
          }).catch(() => "attention" as const)
        : Promise.resolve(null),
    ]);
    // A failed read is reported as signed out, not as "unknown": the capsule
    // would otherwise sit on "checking status" forever, since the per-endpoint
    // catch below resolves instead of raising.
    return { orca: account?.state || "signed_out", orcaName: account?.name || "", web: web || "idle" };
  };
  summaryTail = summaryTail.catch(() => UNKNOWN_ACCOUNT_SUMMARY).then(run, run);
  return summaryTail;
}
