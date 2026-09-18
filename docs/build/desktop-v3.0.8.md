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
for results and limitations.

## Completed Delivery

Published `desktop-v3.0.8` at 2026-09-18 03:01:40 +08:00, targeting
`0b2d30a7d48a675b45024a13bb73c729e99c9034`. The 17 GitHub assets match the local
distribution. Mac now serves the `20260918-orca308/orca` directory; the previous
release remains intact. The signed public manifest, all unique update payloads,
missing-path HTTP 404 responses and the actual Windows updater download passed.
See the linked verification record for timings, hashes and the upload recovery.

## Interrupted Upload Recovery

This release's builds and installer acceptance passed, but the original upload
job returned HTTP 400. Recover the original `dist-*` CI artifacts and verify their
artifact SHA-256 values before unpacking. Do not rebuild an application merely
to retry transferring it. Verify all package signatures with the client's key.

Inspect `GET /repos/{owner}/{repo}/releases/{release_id}/assets`, since the assets
nested in the release response may omit unfinished `starter` entries. Before
retrying, confirm the release is still a draft with the expected tag and source
SHA. Remove only incomplete entries belonging to that draft. Preserve uploaded
assets whose size and digest match; stop on any mismatch. Do not use a blanket
clobber or modify historical releases.

The recovered manifest uses the existing release tool and signing key. Its
signature must verify against the application key. Write `SHA256SUMS.txt` with LF
line endings so macOS `shasum -c` reads the same filenames. Final acceptance still
requires all 17 assets, Mac staging hashes and the real public updater download.
