# Isolated Native Candidate Client

This is a test-only Linux amd64 harness, not a production hook or a substitute
server. `TestActualCandidateAccount` uses public `orcaaccount.Client` APIs and
the actual OpenAI and Anthropic adapters against the independently booted full
candidate, native database, native Redis, and disposable synthetic upstream.
Local safety tests are not evidence of actual-candidate acceptance.

## Parent-Only Execution

The parent owns all remote copying, Docker operations, and cleanup. Run inside
the already owned fixture container; publish no ports. Confucius's server
`dev-docs/desktop-native-stage.md` defines seeding, the exact manifest and controls.
Do not run until the full startup stage and native fixture are ready.

Required environment (identifiers only; no secrets):

```sh
ORCA_ACCOUNT_CANDIDATE=1 ORCA_STAGE_DISPOSABLE=1 ORCA_STAGE_RUN="$owned_run_id" \
  /run/orca-stage/orca-account-candidate.test \
  -test.run '^TestActualCandidateAccount$' -test.count=1 -test.timeout=8m -test.v
```

The existing root-owned, regular 0600 file
`/run/orca-stage/client-input.json` supplies the private cookie, control token,
synthetic subject and dynamic group IDs. The fixture CA is the regular 0600 file
`/run/orca-stage/ca.pem`. Neither file is accepted from argv or printed.
The manifest pins candidate SHA256
`2c7c4252f1794655aac4a0aa6ca1362bae1c87962a8611655f6984b2734108f8`.
The parent must independently hash the actual server executable before running;
the client validates provenance supplied in the manifest, not the server file.
A replacement candidate requires a reviewed pin change and client rebuild.

Absent the explicit execution gate, the actual-candidate test skips. With the
gate set, wrong OS/isolation/run, missing inputs or incomplete controls fail.
The fixture stage hard-caps all upstream attempts at 32, including errors. This
first client pass admits exactly four calls (two successful, two canceled);
the local transport refuses more than eight inference or 256 total requests.

## Assertions

- Real consent denial and allowance through the candidate form and public
  Client.Login; S256, state, nonce and exact numeric ephemeral callback binding.
- Public refresh, profile/catalog/wallet, exact group-scoped restricted
  credentials, detached DTOs, Close/Restore of the same grant, revoke/logout.
- Real adapters parse successful synthetic SSE for both protocols. No secrets
  in provider config; Credential.HTTPClient supplies runtime authorization.
- Exactly one ingress and upstream call per idempotency identity. The fixture
  adds ID-only metadata before native dispatch; adapter bodies are unchanged.
- Captured X-Request-Id must query an owned receipt even when the upstream emits
  a deliberately different request header. No fixture response-header rewriting.
- Successful receipts settle at exactly 18 int64 CNY micros; cash/gifts separate.
- Both adapters cancel promptly after the first delta. Native upstream work may
  continue for metering, so require ingress cancellation and bounded upstream
  closure, not immediate upstream-context cancellation. Missing terminal usage
  must remain pending/unknown without a settled amount, including false zero.
- After Close and RAM-session Restore, receipt recovery only queries the server;
  no new inference, no automatic replay. Final fixture totals must remain four.

Only the two fixed HTTPS logical origins are permitted, dialed exclusively to
127.0.0.1:8443 with fixture-CA trust and normal hostname validation. Proxy, system
roots, redirects, connection reuse and implicit HTTP body replay are disabled.
The only plain HTTP exception is the current login's exact loopback callback.

Linux production secure storage stays unsupported. The harness uses an isolated
process-memory Store; it does not test Windows DPAPI, desktop accountProvider,
desktop journal persistence, higher agent-loop recovery, human browser login,
or paid inference. Existing Windows offline integration tests cover DPAPI and
desktop journal flow separately. No full-candidate pass is claimed until the
parent executes this binary and retains its bounded fixture evidence.

## Offline Build

From the worktree root in PowerShell, with dependencies already cached:

```powershell
$env:GOCACHE='D:/AI-Reasonix/.tmp/orca-beta-gocache'
$env:TEMP=$env:GOCACHE
$env:TMP=$env:GOCACHE
$env:GOPROXY='off'
$env:GOSUMDB='off'
go test ./internal/orcaaccount -run '^TestCandidate' -count=20 -shuffle=on
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go test -c ./internal/orcaaccount -o ../orca-account-candidate.test
```

Binary SHA256 must accompany every handoff. Do not include private manifests,
cookies, keys, session bytes or production config in any source artifact.
