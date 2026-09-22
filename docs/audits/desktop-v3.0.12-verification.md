# O.R.C.A. Desktop 3.0.12 Verification

## Fixes and local evidence

Synthetic Agent regressions reproduced false terminal readiness errors before the fix. Five cases now cover recovered actions, rejected checklist updates, rejected sign-off, and the latter two following an earlier action failure. Each checks request counts, one final answer, preserved pending todos and rejection of unsupported completion claims. Existing tests retain terminal errors for real new failed verification.

HTTP/SSE regressions cover delayed consumers, saturated delivery, byte activity in fragmented lines, cancellation, and terminal chunks on a channel left open. Three synthetic Agent turns exercise six HTTP requests with reasoning and split tool arguments. Sustained consumer blockage reports an error without replay and a subsequent request remains usable.

Before release metadata changes, root Go, desktop Go, frontend all-tests and Windows Wails production build passed. The local 3.0.11 preview also passed NSIS CRC, archive extraction and exact embedded-application SHA-256 comparison. This evidence is not a substitute for the new version's CI package checks.

## Release gates

3.0.12 release CI and package verification are pending at this source commit. Publication is permitted only after they pass. Record final CI run, commit, source archive digest, package verification and public update-source checks with the delivered release report.

No user conversations, configuration, production model prompts or service logs were read for this repair. No Computer Use or native workstation interaction was performed. Full native-window interaction, manual multi-display checks, and production-provider certification are not claimed. Platform/installer CI evidence must be distinguished from interactive desktop acceptance.
