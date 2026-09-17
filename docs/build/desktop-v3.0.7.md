# O.R.C.A. Desktop 3.0.7 Release Runbook

Published on GitHub at 2026-09-17T13:22:09Z, followed by the Mac site and signed
stable manifest switch. Immutable release source/tag:
`ee0c92f6cb9888b12f312937d4ac3a195cc981c5`.
Mac directory: `20260917-orca307/orca`; prior `20260917-orca306/orca` retained.
The earlier unsigned 3.0.6 local-fix attachment is superseded by this signed
3.0.7 release. Historical assets and tags were not replaced.

Use the native release-desktop workflow on the exact reviewed commit. All tests,
three-platform builds, Windows installer acceptance, archive checks, and Minisign
signatures must pass before publishing the generated draft.

Verify downloaded assets against GitHub sizes/digests and SHA256SUMS. Stage the
same files on the Mac server in a new release directory, verify server-side
digests, and switch the website and signed stable manifest atomically. Preserve
previous release directories for rollback. Verify public packages and updater
manifest after activation. Windows publisher signing remains unavailable.

Local delivery: D:\AI-Reasonix\dist\desktop-v3.0.7\.

See [verification](../audits/desktop-v3.0.7-verification.md) for test scope and
limits. Never claim a passing helper test alone is a complete installation test.
