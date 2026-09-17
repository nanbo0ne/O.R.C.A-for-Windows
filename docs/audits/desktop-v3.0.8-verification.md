# O.R.C.A. Desktop 3.0.8 Verification

Release preparation, 2026-09-18. Publication is authorized but has not completed.

Modern layout verification: 362 browser cases, 3990 assertions, 38 screenshots,
zero failures. The 60 Classic baseline comparisons have identical geometry and
computed styles. Attachment layout adds 24 passing assertions. See the
[UI verification](2026-09-18-modern-chat-polish.md) for actual scope and limits.

Frontend tests, CSS checks, production build, and the local Windows Wails review
build passed. Native startup, maximize and dock layout were inspected using an
isolated profile. The test profile once reported a state-file replacement access
error; it did not recur on the final review restart. No persistence code changed.

Native release CI, installer acceptance, package integrity, signatures, GitHub
asset checks, Mac staging and public updater verification remain pending. This
record will be updated with results before release handoff. No historical release
asset, signature or tag is to be replaced.
