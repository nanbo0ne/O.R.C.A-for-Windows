# ORCA beta: current server integration and security hold

Date: 2026-11-15. Status: isolated server acceptance passed; NOT deployed or released.

## Latest read-only continuation: 2026-11-16 10:36 CST

The existing SSH identity and strict host verification work in normal host
execution. The restricted execution attempt could not read the identity or
known-host files; it did not justify disabling host checks. Both attempts are
retained as `production-diagnose-20261116-103558.txt` (failed access) and
`production-diagnose-20261116-103632.txt` (successful read-only audit).

Live executable SHA-256, process start, migration checksums and historical
unresolved counts match the earlier audit. Health and identity discovery return
200; desktop session returns 404 and ORCA_DESKTOP_AUTH_ENABLED remains unset.
The 2 forwarding and 30 reconcile records predate the running process, with
zero reservations in the checked last ten minutes. This is not proof that all
ordinary-key/other HTTP work is drained, nor authority to refund old records.

Re-ran the source audit: all 3695 baseline files retained, 3743 candidate files,
48 additions and 10 modifications. Existing migrations and pricing, gifts,
reconciliation, metering and capability policy hashes are unchanged. Candidate
binary remains c87743101c88a5784e274fefcf905bb50daf92b297dbdda07395d8d855c981ae.

No deployment occurred. This remains unfinished rollout work, not lack of
server credentials. Before rollout: close new admission using the existing
maintenance gate, prove current execution has drained across gateway paths,
create/verify on-server backups, apply only the reviewed additive desktop
extension and verify/retain rollback. The old deployment script's last-hour
reservation check alone is insufficient evidence for a no-interruption claim.
Real ORCA account login and authoritative paid reconciliation remain pending.

Later continuation: Windows full/race reruns passed. See
`orca-beta-integration-r2-20261115.md` for exact evidence and retained failures.
Read-only production readiness checks found two forwarding and 30 reconcile
records, all older than ten minutes, with unchanged executable hash. These
counts prove neither execution termination nor a right to refund. No rollout.
The older NOT PASS/Interrupted entries below describe their original runs.

## Current Source, Not The Historical Candidate

The user-authorized read-only review covered the Sub2API and personal-homepage
Codex chats. No messages were sent to those chats. No private conversation data
was used in model requests or fixtures.

- Verified SSH: `ai@linux-ssh.aichat.diy`, Cloudflare Access transport, existing
  SSH key and strict known-host verification under `HostKeyAlias=192.168.31.47`.
  The old direct LAN endpoint must not be assumed reachable.
- Current live executable: `whalestack-prod-sub:/app/sub2api`.
- Live version: `0.2.19-whalestack-request-prices`, commit `groups-20261115`.
- Current source: `C:/Users/16260/Documents/Codex/2026-09-25/https-github-com-ranxi2001-sub2api-releases/work/groups-20261115/candidate/backend`.
- New isolated integration: `D:/AI-Reasonix/.tmp/orca-beta-server-requestprices-20261115/backend`.
- Baseline SHA-256: `36383bd1b9336b58ece37aa69a9935a3877ce15bb78d358eb077e617e950b0d2`.
  Rebuilding the original source with Go 1.27.3 and original flags produced this
  exact hash. The live executable still had this hash after acceptance.
- Internal server candidate SHA-256: `c87743101c88a5784e274fefcf905bb50daf92b297dbdda07395d8d855c981ae`.

The actual discovery URL is `/api/v1/identity/openid-configuration`. It returned
JSON with the current identity endpoints and PKCE S256 support. Root and
`/.well-known` probes were the wrong paths; their failures did not establish an
identity outage. Current discovery still describes confidential-client auth,
not proof that the desktop public-client extension has been deployed.

## Changes

- Imported desktop public-client authorization, grant-bound credentials, model,
  wallet and receipt APIs into the current source, not over the live service.
- Reused the later lifecycle patch found in the old integration directory,
  reviewed and manually merged its conflicts with current gateway/store/attempt
  code. The original directory was not changed.
- Desktop execution slots are released only after observed local transport
  cleanup. Incomplete financial usage remains unknown/reconcile with its hold.
  No timeout refund, maximum charge, inferred final token count or request replay.
- Ordinary-key concurrency, cash/gift accounting, quota exposure and existing
  pricing rules remain unchanged. Desktop unresolved-record backlog has a
  separate bound of 64; it is not a fee policy or an unlimited retry permission.
- Existing crash/DB-failure cases without termination proof remain fail-closed.
  No automatic authoritative final-receipt recovery SLA has been established.
- Preserved current response-model mapping, request-based tier pricing,
  reasoning-only/empty-output rules and Chat service-group restrictions.
- Desktop model metadata now includes the server's confirmed reasoning fields.
  Client wire DTOs retain these fields and exact per-request/long-tier prices.
  This does not claim that all new reasoning controls or Responses-protocol UI
  integration are complete. Final charges still come only from server receipts.

The source audit records 3,695 baseline files, 3,743 candidate files, 48 additions
and 10 modified original files. Existing SQL migrations and protected pricing,
gift, reconciliation, meter and model-capability policy files are unchanged.
See the stage's `evidence/source-audit.json` and source manifests.

## Verification This Run

All server integration used new labelled PG/Redis containers, internal networks,
synthetic users and local synthetic upstreams. No production mounts, published
ports, real API keys, private messages or paid model requests. Both harnesses
reported cleanup success. Their reports were retrieved locally.

| Check | Result | Scope |
| --- | --- | --- |
| Windows identity/desktop/wallet unit suite | 3 packages, 178 pass events | 55 opt-in integration skips, separately exercised below |
| Fresh PG/Redis suite | 90 top-level tests, 224 pass events, no fail/skip | auth, groups, wallet, request tiers, slots, replay, cancellation, Linux cleanup |
| Actual server binary + ORCA account client | 34 top-level tests, 165 pass events, no fail/skip | synthetic consent/PKCE, native adapters, startup and rollback |
| Actual client inference cases | 4 synthetic requests | OpenAI success/cancel; Anthropic success/cancel; exact receipts and no replay |
| Client account package after DTO changes | 107 pass events | 1 opt-in actual-candidate test, passed in Linux harness above |
| Source whitespace checks | No diagnostics | app `git diff --check` exit 0; server no-index check exit 1 denotes source differences |
| Windows desktop full suite | NOT PASS | antivirus deleted the main test binary; 3 browserhost path-access failures also remain |
| New server Windows race run | Interrupted, NOT PASS | stopped after the antivirus report, before test results |

Evidence root: `D:/AI-Reasonix/.tmp/orca-beta-server-requestprices-20261115/evidence`.
PG run: `orca-requestprices-test-20261115T175631-90fc9a18`.
Actual-binary run: `orca-requestprices-test-20261115T180534-40f82298`.
Some inherited fixture labels say `account30`; this run's baseline binary is the
byte-identical November 15 production version, not the old account30 executable.
The old Linux-specific Windows failures are preserved in `unit-tests.jsonl`.
They passed in Linux; the model-catalog mock was updated to current SQL arguments.

## Antivirus Event At 18:07

The user supplied a security log showing `Trojan/Loader.bb` and deletion of
`C:/Users/16260/AppData/Local/Temp/go-build2086128495/b001/desktop.test.exe`
at 18:07:00. The operation was a write by the Go linker, parent `go.exe`.
The desktop test log at 18:07:01 reports inability to launch that exact path.
This identifies the affected artifact as this run's generated test executable,
not the installed ORCA main program. This test invocation did not launch it.

The local `go.exe`, `compile.exe` and `link.exe` match their corresponding entries
in the cached Go 1.27.3 toolchain ZIP. This is local cache consistency, not an
independent malware verdict. The deleted test executable is unavailable for
hashing or rescanning, so its false-positive status is NOT confirmed.
Go documents Windows false positives at https://go.dev/doc/faq#virus, but that
general explanation does not certify this particular detection.

Related Windows executable/race testing was stopped. No antivirus exception,
disable, quarantine restore, repacking/renaming workaround or public sample
upload was attempted. Earlier `pwsh.exe` system-immunity entries in the screenshot
are separate events; their causes were not inferred from this detection.

## Remaining Gates

1. Resolve the Windows test-binary detection and path-access failures without
   weakening protection; rerun the affected suites and relevant races.
2. Recheck current deployment topology, source/migration drift and in-flight work
   before a compatible server rollout. Do not execute the inherited old
   deployment commands or overwrite production with historical artifacts.
3. Human ORCA login, live user-authorized receipt reconciliation, modern/classic
   native clicks and installation/coexistence still need end-to-end acceptance.
4. DeepSeek Web's remaining complex-task/continuation cases and mixed-source
   main/sub-agent usage retain their earlier gaps; this server run did not retest
   them or reset their authorization.
5. Incomplete financial usage still needs authoritative reconciliation. Freeing
   an execution slot does not mean refunding the held amount or completing a bill.

No production deployment, client release, update-source change or new installer.
The earlier `dist/orca-beta-3.0.14-beta.2` source/evidence delivery is preserved and
does not include this later addendum/DTO change. No new all-green release claim.
