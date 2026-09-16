# O.R.C.A. 3.0.5 Verification

Date: 2026-09-17 (UTC+8). This report separates live DeepSeek calls, local
fixtures, rendered browser checks, and native release checks. No user screenshots
or credentials were included in test artifacts.

## Live DeepSeek Tests

Tests used the official Chat Completions endpoint and credentials supplied through
the local credential store. The overall cap was 40 attempts or CNY 5. Each attempt
reserved its maximum input/output cost at peak prices before sending; reservation
is a conservative limit, not the actual bill.

| Scenario | Result | First observed stream delta |
| --- | --- | --- |
| Flash low, Unicode output | Pass; canonically equivalent Unicode accepted | 188 ms |
| Flash high, Unicode output | Pass | 135 ms |
| Flash max, Unicode output | Pass | 112 ms |
| Direct native vision, random code and bar chart | Pass, exact visible code and tallest bar | 141 ms |
| Two dependent tool calls, final answer, next question | Pass, receipts verified; no tool replay | 104-187 ms |
| Pro text request | Pass | 107 ms |
| Controller -> main Agent -> task(images) -> vision child -> main Agent | Pass, three HTTP 200 requests | 643 ms |

The final scenario generated a Chinese image with a random code inside an isolated
workspace. It verified native image parts, the exact attachment snapshot bytes,
one task dispatch, three usage receipts, and the parent's final answer. Its model
capability was explicitly configured in the fixture; it exercises the actual
controller and task path but is not an application startup/UI test.

The tool-history test inspected outgoing request JSON and matched original
reasoning separately from Hook-transformed display text. The real service returned
empty reasoning on some final answers; that legitimate empty value survives the
next tools-bearing request. Non-tools requests omit historical reasoning.

There were 17 attempts in total, including the initial HTTP 402 before the account
was topped up. The final cumulative reservation was CNY 3.321856. No further live
calls were required. The first 13 successful requests cost approximately CNY
0.010393 using reported usage and the applicable off-peak prices. The three child
scenario requests added 2,311 input and 333 output tokens; their report does not
separate cache hits, so a precise combined bill is not claimed.

Two harness assertions were corrected after observing real responses: Unicode
NFC equivalence, and valid empty reasoning. Neither correction synthesizes output
or bypasses the exact image, tool receipt, or protocol checks.

Local evidence: `.tmp/v305-live-results-funded2.json`,
`.tmp/v305-live-results-tool-pro.json`, `.tmp/v305-live-results-child.json`.

## Local Regression

- Root `go test ./... -count=1`: passed, 56 packages with tests.
- Configuration: one-time migration, completed no-op migration, exact provider
  identity, project credentials, manual Pro after restart, backup failures,
  unreadable sources, explicit vision/context overrides, and strict replacement.
- Protocol fixtures: reasoning effort JSON, all assistant messages with tools,
  Hook separation, session restoration, compaction, cancellation, and terminal
  handling of an upstream reasoning-history rejection.
- Desktop migration: authoritative disk snapshot, concurrent edits, partial
  project failures, preservation of unknown state and session references.
- Frontend model tests: custom endpoints and manual capabilities retain their
  identity and values; legacy Flash names deduplicate within a provider.
- Windows payload preparation: pinned Node and CodeGraph archives verified.

## Release Checks

Release source: `db3b3b5c9a4ca81cc310dd8dfc9a42188df87e75`.
[Workflow 35131853347](https://github.com/nanbo0ne/O.R.C.A-for-Windows/actions/runs/35131853347)
passed the core suite, frontend checks, Linux race tests (agent, control, billing,
local AI, configuration), and native Windows/macOS/Linux builds and tests.

Hosted Windows installer acceptance passed installation of the pinned 3.0.4
baseline, upgrade to 3.0.5 using the stored installation path, a separate explicit
installation directory, payload comparisons, and uninstall. Synthetic configuration,
session, and model markers were retained. The user's installation was not changed.

All 17 downloaded release files matched GitHub size/digest metadata; 16 checksum
records and all eight payload/manifest Minisign signatures passed. All seven
package archives passed 7-Zip integrity checks. The final CI installer is
88,804,245 bytes with NSIS CRC `2c072443`; installer and portable executable bytes
match and report version `3.0.5.0`. This supersedes the local candidate's size/CRC.
Final delivery details are in the [release runbook](../build/desktop-v3.0.5.md).

After publication, the public manifest and signature matched the release assets.
The application's actual updater detected 3.0.5 from version 3.0.4 and downloaded
the complete Windows installer from the Mac source, verifying its signature and
SHA-256. Independent public downloads also verified the Windows portable ZIP,
macOS DMG, and Linux DEB; missing update and package URLs returned 404. This probe
used an isolated directory and did not install or restart the user's application.

## Rendered Interface Checks

The final rendered regression passed 224 scenarios and 1,148 assertions across
Modern/Classic, Chinese/English, widths 760/900/1024/1180/1181/1366/1920, official
and long custom model labels, and four sidebar combinations. Classic header
clipping and status collisions found during this run were fixed with scoped CSS.
There were no remaining measured overlaps/clipped controls or browser errors.
Evidence: `.tmp/v305-ui/layout-fix/results.json` and its screenshots.

The frontend suite passed with 578 PASS lines; production build passed. Its main
bundle remains 991.96 kB (284.26 kB gzip), above the 600 kB build warning threshold.
Rendered checks used isolated mock data, 900px height, and DPR 1; they are separate
from the live API and native platform tests above.

Native DPI, computer control, and managed local AI are outside this
release's test scope; the latter two remain disabled. Minisign authenticates
updates and must not be described as a Windows publisher signature or macOS
notarization.
