# O.R.C.A. Desktop 3.0.7 Verification

Publication remains gated on the native release workflow and delivery checks.

Local regression exercises the actual NSIS helper using isolated 64-bit,
windowless processes. Cases cover automatic target termination, preservation of
an unrelated same-name process, unreadable paths under redirected PowerShell,
an external exclusive file lock, and retry after the lock is released.

Hosted Windows acceptance installs the pinned public 3.0.5 baseline, substitutes
a synthetic background executable in the owned installation directory, then
performs a full upgrade while that executable is running. It checks installed
payload hashes, metadata, synthetic user data retention, an explicit alternate
install directory, and uninstall. This is not a live-user-task recovery test.

The installer is not transactional: disk exhaustion or a new file lock during
copy can still abort a partially completed installation. Files cannot be skipped;
retry the installer after resolving the cause. Preflight checks do not constitute
a guarantee against filesystem changes after checking.

Manual upgrades request normal window closure and allow 30 seconds before
terminating remaining exact-path processes. This cannot guarantee preservation
of an unsaved active task; save tasks first. In-app updates separately require
idle work and persist session snapshots before quitting.

Do not execute the hosted installer acceptance harness on a user workstation.
Do not overwrite published 3.0.6 packages, signatures, or tags.
