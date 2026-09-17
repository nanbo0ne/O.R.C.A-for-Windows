# O.R.C.A. Desktop 3.0.7 Release Runbook

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
