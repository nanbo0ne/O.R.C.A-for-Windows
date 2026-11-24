# ORCA Desktop Account Contract

Isolated client. No legacy identity changes, UI bindings, history mutation, live
network tests, or deployment. Fixed public client `orca-beta`; issuer
`https://sub.aichat.diy`; API prefix `/api/v1/desktop`; router
`https://router.aichat.diy`. All success bodies are raw JSON, not envelopes.

## Wire Types

The exact JSON tags are in `types.go`.

* `GET /authorize`: `response_type=code`, `client_id`, `redirect_uri`, `state`,
  `nonce`, `code_challenge`, `code_challenge_method=S256`, `scope=desktop`,
  `device_id` (16-128 URL-safe chars), optional `device_name`.
  Redirect is `http://127.0.0.1:{ephemeral}/callback`. The server must validate
  numeric loopback, exact path, no fragment/userinfo/query, and permit its port.
* `POST /token`: form-encoded standard `authorization_code` (`code`,
  `code_verifier`, `redirect_uri`, `client_id`) or `refresh_token`
  (`refresh_token`, `client_id`). Never a client secret.
  JSON: `{access_token,refresh_token,token_type:"Bearer",expires_in,subject,issuer,nonce,scope:"desktop",session_id,device_id}`.
  `expires_in` is integral seconds, at most 300. Both tokens and a stable string subject are
  required. `nonce` must equal the value stored with the code on exchange;
  refresh must preserve that original nonce, subject, session_id and device_id.
  Refresh tokens must rotate. The issuer field is the HTTPS origin above.
  This is a desktop OAuth contract, not client-side unverified ID-token parsing.
* `POST /revoke`: form `token`, `token_type_hint`, `client_id`; 200/204 success.
* `GET /session`: `{subject,display_name,email?,groups:[{id,name}],session_id,device_id,expires_at}`.
  The additive `user:{id,username,email}` alias is also accepted. Dates are RFC3339.
* `GET /models`: `{currency:"CNY",models:[{id,name,group_id,group_name,publication_id,protocol,endpoint,capabilities,prices,pricing_version}]}`.
  `publication_id` is a string. Protocol is `openai` or `anthropic`; endpoint
  is independently significant (`/responses` is not `/chat/completions`).
  Capabilities `reasoning`, `vision`, `context_window` are nullable. Nil means
  unknown, never inferred from the model name. Exact price types are in Go.
* `GET /wallet`: `{currency:"CNY",cash_micros,held_micros,available_micros,scoped_gifts:[{id,group_id,balance_micros,held_micros,available_micros,expires_at?}]}`.
* `GET /requests/:id`: `{request_id,status,currency:"CNY",estimated_micros?,settled_micros?}`.
  Status is `estimated`, `pending`, `settled`, or `unknown`. Absence of an amount
  is unknown, not zero. Settlement is authoritative only when `settled`.
* `POST /credentials`: JSON `{group_id?:int64}`;
  response `{credential_id:string,api_key,base_url:"https://router.aichat.diy/v1",group_id:int64,restricted:true,expires_at:RFC3339}`.
  A requested group must match exactly; restrictions are enforced by the server.
  Omission selects only when exactly one permitted group exists, never unrestricted.

REST endpoints require account access-token Bearer authentication. All monetary
values are signed int64 CNY micros, including exact values above 2^53. Cash and
group-scoped gifts remain separate. Do not coerce these values through JS Number
in a UI bridge. Errors must not echo codes/tokens; the client never surfaces raw
error bodies, URLs, or transport errors.

Multi-group gifts may be repeated with the same grant `id`, plus `group_ids`.
Do not sum duplicate grant IDs across scopes. Callback `iss` is mandatory and
must be the fixed issuer. Both state and nonce are cryptographically random.

## Parent Integration API

`New(Options{Store: store, OpenBrowser: opener}) (*Client, error)`;
`NewSystemStore(absolutePath) (Store, error)` uses current-user DPAPI on Windows.
An empty browser opener uses the system browser. Non-Windows storage explicitly
fails with `ErrUnsupported`; there is no file plaintext fallback.

UI-safe methods: `Snapshot() Snapshot`, `Login(ctx) (Profile,error)` (also account
switch), `Restore(ctx) (Profile,error)`, `Session(ctx) (Profile,error)`,
`Models(ctx) (Models,error)`, `Wallet(ctx) (Wallet,error)`,
`Request(ctx,id) (Settlement,error)`, `Refresh(ctx) error`, `Logout(ctx) error`.

Backend-only: `Credentials(ctx, CredentialOptions) (*Credential,error)` returns
an opaque runtime capability. `Credential.Info()` is UI-safe.
`Credential.Do(req) (*http.Response,error)` attaches the API key only to the
fixed router origin, refuses redirects, and is invalid after account changes.
`Credential.HTTPClient() *http.Client` can be passed directly to
`provider.Config.HTTPClient`. It preserves Idempotency-Key and X-Request-Id,
strips conflicting x-api-key/cookies, and does not expose a raw token getter.
Router errors preserve status and correlation headers with an empty body, so
providers classify 401/403 without retrying them as transport errors or logging
potentially reflected credentials. There is no application-level retry here.
No raw token/key getter exists. Do not bind Client/Credential/Store to Wails.

`CurrentSessionGeneration() uint64` and
`GenerationContext(ctx, generation) (context.Context, context.CancelFunc, error)`
let the parent bind provider work to the current account. Login and Logout cancel
old-generation work automatically. `CancelContexts() error` is a local-only
sign-out (cancels and clears credentials); use Logout to revoke remotely too.

`Close() error` is application shutdown: idempotent, cancels account/browser/
router contexts, clears memory, preserves the encrypted store unchanged, and
does not revoke. The instance then returns `ErrClosed` for new operations; use
a new Client and Restore at next launch. If shutdown interrupts refresh rotation,
the saved token may already be consumed server-side, requiring sign-in again.
Close never retries that uncertain rotation or permits its late response to save.

Use a dedicated beta user-data path supplied by the parent for secure storage.
One Client owns one store path. Authentication errors never delete conversations;
the package does not know where conversation history is stored. Logout cancels
in-flight account/router operations before attempting remote revocation, erases
local credentials even when offline, and reports storage/revocation failure.

## Owned Files And Verification

Only these 13 new files belong to this client subtask:

* `client.go`: options, private session state, storage boundary, restore/refresh,
  generation guards, cancellation, logout, shutdown.
* `oauth.go`: S256/state/nonce and one-use numeric-loopback callback lifecycle.
* `http.go`: bounded raw-JSON/form API requests, safe error classification.
* `browser.go`: system-browser URL handler, no embedded web view or shell.
* `types.go`: typed session, model/capability/price, wallet, receipt DTOs.
* `api.go`: authenticated REST methods and opaque router-only runtime transport.
* `store_windows.go`: atomic current-user DPAPI ciphertext storage.
* `store_other.go`: explicit unsupported platform, no plaintext fallback.
* `client_test.go`, `security_test.go`: synthetic httptest and memory-store tests.
* `store_windows_test.go`, `store_other_test.go`: platform storage checks.
* `CONTRACT.md`: wire contract and integration handoff.

2026-11-06 verification: 29 Windows top-level tests plus subtests passed 50
shuffled repetitions, statement coverage 82.6%; `go vet` passed; Linux amd64
test binary cross-compiled (not executed). Current-user DPAPI tests used only
synthetic data in test temporary directories. Race detection could not run:
CGO is disabled by default and enabling it reports missing `gcc`. No external
requests, personal-data fixtures, UI/server edits, deployment, or new modules.
