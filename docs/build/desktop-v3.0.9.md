# O.R.C.A. Desktop 3.0.9 Release Runbook

Promote the reviewed installer repair to the normal desktop release matrix.
The standard NSIS/MUI2 wizard replaces the rejected custom installer design.
Production builds omit ORCA_PREVIEW; do not rename a preview installer and claim
its caption was changed. Re-run Windows acceptance against the production file.

Dispatch release-desktop from the reviewed main commit with stable channel,
tag desktop-v3.0.9, and the existing explicit unsigned-Windows policy. Minisign
is required. It is not Authenticode or Apple notarization. Keep the generated
GitHub Release a draft until validation is complete.

Require core/race/frontend tests, three native platform builds and desktop tests,
Windows installer acceptance against the pinned public 3.0.8 baseline, package
integrity and version checks, and verification with the application's Minisign
public key. Run the installation harness only on disposable GitHub-hosted
Windows, never a user's workstation.

Save verified delivery assets to D:\AI-Reasonix\dist\desktop-v3.0.9. Verify
GitHub asset sizes/digests, SHA256SUMS, signatures and installer CRC. Use the
exact accepted build outputs; retry transfers without rebuilding binaries.

Stage packages and website under a new Mac release directory. Check every hash
before switching the website symlink and signed stable manifest atomically.
Retain 3.0.8 and its directory for rollback. Confirm public JSON, signature,
package downloads, missing-path 404 and the actual updater. Do not modify other
sites, old releases, tags, user data or the unrelated community data change.

Preview evidence: Windows acceptance run 35320328576 passed on source
4626f7047aa76132eac8e1dd87ab2a64fb760be6. That proves the preview only;
formal build results are recorded separately in the release verification report.
