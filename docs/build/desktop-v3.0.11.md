# O.R.C.A. Desktop 3.0.11 Release Runbook

Release `desktop-v3.0.11` from the reviewed commit only after the full core,
frontend, desktop, packaging, installer, signature, and update-source checks
are green. Do not include `site/src/data/community.json` or any user data.

Required gates:

- `go test ./... -count=1 -p=1`.
- Desktop tests, changed-package race tests, frontend tests, CSS checks and production build.
- Windows NSIS installer, portable archive, fresh install, 3.0.10 upgrade, custom path and uninstall retention.
- macOS Universal and Linux amd64 packages, archive integrity and platform signatures.
- Signed manifest, SHA-256 records, Minisign verification and exact GitHub asset matching.
- Mac update-source staging, public manifest validation, missing-path 404 and isolated updater download verification.

Native GUI interaction remains outside the automated release gate while Computer
Use is disabled and the workstation is in use. Record that boundary explicitly;
do not claim native-window acceptance from headless or hosted-runner tests.
