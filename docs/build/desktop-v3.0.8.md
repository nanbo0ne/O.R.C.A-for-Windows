# O.R.C.A. Desktop 3.0.8 Release Runbook

Use the native release-desktop workflow on the reviewed source commit, dispatched
from main with `tag=desktop-v3.0.8` and the existing explicit unsigned-Windows
policy. The workflow generates a draft; publication follows validation.

Require core/race/frontend tests, all three native builds and desktop tests,
Windows installer upgrade/data-retention/uninstall acceptance, archive checks,
and Minisign verification. Hosted installer acceptance uses the pinned 3.0.5
baseline; do not run that harness on a user workstation.

Download every asset to `D:\AI-Reasonix\dist\desktop-v3.0.8\` and verify sizes,
GitHub digests, SHA256SUMS and signatures. Stage the exact files in the Mac release
directory `20260918-orca308/orca`, retaining `20260917-orca307/orca` for rollback.
After validation, publish the GitHub draft and atomically switch the Mac website
and signed stable manifest. Verify public packages and the actual updater.

Do not overwrite existing release assets or tags. Update signing is distinct
from OS publisher signing. See [verification](../audits/desktop-v3.0.8-verification.md)
for results, pending checks and limitations.
