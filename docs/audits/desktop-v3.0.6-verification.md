# O.R.C.A. Desktop 3.0.6 Verification

Date: 2026-09-17 (UTC+8). Tests below distinguish browser fixtures, actual local
model traffic, and native release gates. No user screenshots or conversations
were sent to any model. Credentials do not appear in reports or the repository.

## Reproduced and Fixed

- New custom-provider forms displayed OpenAI-compatible but initialized their
  protocol from the first registered kind. The sorted registry begins with
  Anthropic. Both saving and catalog discovery now use the displayed OpenAI kind;
  editing an existing Anthropic provider preserves its explicit protocol.
- Committed reasoning-only messages split neighboring tool calls into multiple
  visible groups. They now remain activity details in a single counted group.
  Real progress replies and live consumers remain boundaries, so streaming text
  is visible before the final Message event. Completed final answers stay outside
  the collapsed process summary.
- Modern markers and their hover hit area now own a gutter separate from the
  transcript. Classic CSS, window layout, and disabled-feature boundaries remain.
- Switching a provider protocol drops stale incompatible effort/thinking options;
  restoring a tab also validates its prior effort against the selected provider.
  Legitimate Anthropic and custom-supported effort values remain intact.
- Recognized full API endpoints are normalized without losing proxy prefixes.
  Structured unsupported-route errors identify the protocol and POST path instead
  of labelling every HTTP 400 a program defect. No cross-protocol request replay.

## Actual Local Model Test

The deployed Lens catalog exposes `Deepseek_Orca_uncensored`, matching the user
report. An isolated executable built from the real Orca Provider adapters ran on
the Mac server. A loopback-only test proxy injected the existing internal service
channel header; that credential stayed on the server. This validates provider
serialization, SSE parsing, and tool continuation, not end-user Key provisioning.

| Synthetic request | Observed result |
| --- | --- |
| Anthropic Messages, same model | HTTP 400, `/v1/messages`, structured unsupported endpoint; reproduces the reported failure |
| OpenAI Chat Completions, short exact response | PASS; 7 text chunks, 6.655 seconds |
| One `echo_code` call followed by its result | PASS; validated argument and final answer, no repeated tool; 12.105 seconds |

Only three successful model calls were made (one plain, two tool-continuation),
plus the deliberately rejected route and catalog reads. No computer inputs, real
file changes, or model/server reconfiguration were involved. The local machine's
saved legacy tunnel hostname separately returned DNS failure, and its saved key
did not match the current Lens key records. It was not silently moved to another
host. Reconnect that access entry using the current endpoint and a valid user key.

Evidence: `.tmp/release-v306/local-model-live.log`. The official public service
health endpoint reports `https://lens.aichat.diy/v1` as its OpenAI base URL.

## Local Regression

- Root `go test ./... -count=1 -p=2`: PASS.
- Desktop `go test ./... -count=1`: PASS after aligning Windows metadata to
  `3.0.6.0`; desktop package 23.146 seconds. Initial metadata mismatch was detected
  by the existing strict contract; the test was not relaxed.
- Frontend `npm run test:all` and production build: PASS, 633 PASS lines including streaming.
- Rendered layout: 127 cases, 733 assertions, no final failures. Modern/Classic,
  narrow and wide windows, panel combinations, hover/click/keyboard navigation,
  scrolling, and empty-state centering; actual components with synthetic content.
  The old CSS reproduced 7px overlap at 1280px with both panels docked; the new
  markers leave 22px clearance. Classic geometry and computed styles match the
  previous baseline. Existing responsive behavior closes both panels at compact
  widths; requested and actual panel states are recorded separately.
- Provider editor browser regression: 27 assertions, including an old-code negative
  control that displayed OpenAI but sent Anthropic. Fetch/save/reopen and existing
  Anthropic preservation passed.
- Timeline browser regression: counted groups, child-call deduplication, phases,
  failures/cancellation, detailed reasoning, collapse state, and pre-Message live
  text passed.
- Endpoint tests verify actual HTTP paths, headers, payloads, error classification,
  no retry after unsupported routes, and preservation of authentication/parameter
  errors. Custom aliases do not imply official DeepSeek capabilities.
- Independent review caught an explicit `models_url` query regression. A new
  failing test reproduced it; exact catalog URLs now retain their path/query,
  while API bases still reject ambiguous queries. Provider/config/control suites
  passed again after the correction. Failed catalog requests do not echo query
  credentials in transport errors.

Evidence: `.tmp/release-v306/{root-tests,desktop-tests-final,frontend-tests,frontend-build}.log`
and `.tmp/release-v306/ui/`. Browser tests use DPR 1, not native system DPI.
The existing main-bundle size warning remains: 991.99 kB, 284.26 kB gzip.

## Release Gates

Three-platform native builds/tests, Linux race tests, hosted Windows 3.0.5 upgrade
and uninstall/data-retention acceptance, package signatures/digests, and public
update/download checks must pass before public activation. Final workflow and
delivery results are recorded in the [release runbook](../build/desktop-v3.0.6.md).

Windows remains without Authenticode signing and macOS remains unnotarized.
Minisign verifies update integrity, not operating-system publisher trust.
