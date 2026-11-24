# ORCA beta R8: gap triage and webpage access

Date: 2026-11-16. Internal continuation, not a release certificate.

Latest update: actual R9 current-login adoption succeeded. A new read-only
Agent task received one file and automatically returned its result, but the
next response failed on raw_fragment_type_unknown. Post-R9 source fixes a
separate premature stop-observer cleanup bug and passes browser/host race
regressions; the actual unknown fragment is unresolved. Read the account-switch
report for the latest evidence. No new candidate was built.

Earlier update: R8 native UI passed; the actual webpage was signed out. The user signed
in personally, and the subsequent recheck returned the old-identity boundary.
R9 source fixes adoption of the current login while retiring old contexts.
Read `orca-beta-account-switch-20261116.md` for newer evidence, including full
Go results and the synthetic R9 native matrix. The original diagnosis below is
retained chronologically, not as a claim that the cause remains unidentified.

## What actually prevented progress

1. Web source: an old `send_unconfirmed` state remains. The user tried the
   recovery button today and it failed. R7 discarded all error categories in the
   settings UI, displaying only a generic retry message. This is a diagnostic
   defect, not evidence of missing user consent or an expired login. Existing
   login and send authorization have not been reset. The live failure cause
   still needs to be identified; do not count recovery as passed.
2. Account and billing: the compatible server candidate passed earlier isolated
   acceptance but was not deployed. This was unfinished rollout investigation,
   not a demonstrated lack of server access. Current health is 200; the desktop
   session route is 404 and ORCA_DESKTOP_AUTH_ENABLED is not set.
3. Installation/coexistence: synthetic installation checks exist, but real
   installation, upgrade and uninstall testing was not completed. Read-only
   inventory found no VirtualBox/VMware installation, and Hyper-V plus Windows
   Sandbox are disabled. VirtualMachinePlatform alone is enabled. No Windows
   feature, security setting or installed release was changed.
4. Windows tests: earlier executable launch failures were real, but should not
   all have been attributed to antivirus. Today the same R7 EXE failed its
   WebView2 children inside restricted execution and launched successfully in
   normal host execution. Ordinary full Go suites subsequently passed in the
   isolated host context. This does not retrospectively explain every prior
   deleted/denied executable or certify an antivirus false positive.

## Changes under test

- Added Open webpage beside Log out of DeepSeek in the existing settings
  controls. It uses the existing native page and returns to settings on close.
  It does not configure, recover, revoke consent, log out, or send a request.
- Recovery now emits fixed public categories for busy, disconnected, identity
  change, signed-out, challenge, restriction, rate limit, adapter mismatch,
  not-ready and timeout. Arbitrary transport messages, URLs and credentials are
  not displayed. Recovery timeout increased from 3 to 15 seconds; no retries,
  request replay or automatic authorization were added.
- No transcript, cookies or credential files were inspected. Only the approved
  synthetic beta profile was launched. No paid model requests occurred.

## Evidence already completed

- Ordinary desktop Go: 802 top-level pass, 18 opt-in skips, zero fail, exit 0.
  Evidence: `.tmp/integration-20261115-r2/desktop-081638-eb23be1d`.
- Ordinary root Go: 2385 top-level pass, 35 opt-in skips, zero fail, exit 0.
  Evidence: `.tmp/integration-20261115-r2/root-082253-5352a536`.
- New recovery classifier: known states and unknown/private-data suppression
  passed. Evidence: `desktop-083054-a6fefb6a` in the same directory.
- Real React clicks: Modern/Classic x Chinese/English, 4 cases, 108 assertions,
  12 screenshots, no failures. Evidence: `beta-recovery-r8` in the same directory.
  Screenshots inspected for Chinese Modern and English Classic. Mock transport
  remains distinct from native WebView2 and real model acceptance.
- An initial frontend typecheck rejected Object.hasOwn under the existing TS
  target. Fixed with the compatible hasOwnProperty call; not a target upgrade.

## Production read-only findings

Evidence: sibling stage `orca-beta-server-requestprices-20261115/evidence/`,
latest `production-diagnose-20261116-084102.txt`.

- Live executable remains SHA-256
  `36383bd1b9336b58ece37aa69a9935a3877ce15bb78d358eb077e617e950b0d2`.
- Container started 2026-11-15 08:35:59 UTC. The two forwarding records were
  created on November 4. All 30 reconciliation records predate this process,
  the newest being November 12. No new reservation was seen in either checked
  ten-minute window. These are not proof of current-process execution; they are
  also not authority to refund, settle, replay or change historical balances.
- The 23 existing compatibility migration checksums exactly match the candidate
  source after the server's whitespace normalization. Both new desktop migrations
  are absent from production. They add delegated-key and execution-lifetime
  fields, not price or balance changes.
- The earlier deployment script itself preserved the same two historical stuck
  forwarding records. Requiring all historical unresolved billing to disappear
  was not an adequate deployment criterion. Next rollout must instead verify
  admission drain, back up on-server, preserve historical financial/key data,
  enable only the desktop extension, and retain tested binary rollback.
- No deployment, service restart, model restart, credential export, financial
  mutation or client publication was performed during this diagnosis.

## Still open

- Current-login adoption in R9 is complete. Resolve the new raw-fragment
  failure, then finish the real Agent matrix without suppressing failures.
- Execute the reviewed compatible server rollout and human account login,
  followed by bounded paid inference and receipt reconciliation.
- R9 provenance and actual adoption passed; post-R9 source changes need a
  fresh reviewed candidate and live regression before being called accepted.
- Prepare an isolated Windows installation environment; do not uninstall the
  user's working release or call synthetic checks a real installation pass.
