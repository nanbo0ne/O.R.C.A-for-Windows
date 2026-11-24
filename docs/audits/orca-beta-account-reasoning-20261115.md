# ORCA beta: account reasoning integration follow-up

Date: 2026-11-15. Scope: current beta source, synthetic fixtures only.
This is not release approval, a malware verdict or live billing acceptance.

## Changes

- Validate confirmed/unknown server reasoning metadata, sources, exact levels,
  default membership, duplicates and revision bounds before accepting a catalog.
- Populate the account model's menu and runtime capability from current server
  metadata. Unknown does not become a guessed capability based on model names.
- Preserve the user's explicit preference, but refresh the supported levels and
  default. Legacy catalogs without this metadata retain their former behavior.
- Carry the explicit/Auto distinction through controller construction. Recheck
  current capabilities on each account request before acquiring credentials.
- Auto uses the current valid server default or omits reasoning effort. Removed
  explicit levels fail with an actionable error and no inference request.
- Disabled-thinking auxiliary requests omit reasoning parameters without changing
  the session preference. Provider config maps are copied, not mutated in place.
- Scope is the supported OpenAI Chat Completions account route. Anthropic remains
  unchanged; Responses-only models are still explicitly unsupported, not silently
  redirected to Chat Completions.

## Current Evidence

Root: `.tmp/integration-20261115-r2/` in the beta worktree.

| Run | Result | Limits |
| --- | --- | --- |
| `root-184025-8e3fd678` | 59 packages; 2,375 top-level tests / 3,483 pass events; zero failures | 42 skip events; opt-in/live tests are not claimed |
| `desktop-183958-2e9d0f66` | 12 packages; 795 top-level tests / 1,183 pass events; zero failures | 19 skip events; no human login or real model inference |
| `root-183815-3f0cd18c` | 4 race-tested packages; 430 top-level / 683 pass events; zero failures | account, config, OpenAI and Web beta; 3 skip events |
| `frontend-tests.txt` | `npm run test:all` exit 0 | synthetic frontend/unit tests, not native interaction |

New wire tests use a local HTTP server and the real OpenAI serializer. They
assert exact-case model identity, xhigh, explicit none, current Auto default,
omission, disabled thinking, stale choice rejection, no stale DeepSeek field,
and no shared-config mutation. Catalog refresh tests preserve explicit selection
while replacing stale capabilities. A denied selection creates neither a
credential nor an inference request in the integrated account fixture.

## Windows Execution

Earlier default-token and full-token retries at 18:34/18:36 failed before the
main desktop test executable could start (`Access is denied`). They remain in
the evidence directory. The user then reported temporarily disabling antivirus.
The next normal isolated full tests executed and passed; the assistant did not
change security settings, restore quarantine or rename a binary to evade a check.
This correlation does not independently prove the original detection false.

## Open Gates

Later checks are recorded in `orca-beta-integration-r2-20261115.md`: the
desktop/server race reruns and 16 current-candidate native fixture combinations
passed. The real code task is waiting for file approval, not completed.

Current server integration/isolated PG and Redis evidence remains in
`orca-beta-server-20261115.md`. No production rollout, real login, paid request,
settlement or installer coexistence result is established by this follow-up.
Native current-candidate interaction, the remaining real Web Agent matrix and
safe old-conversation handoff remain separate. Prior dist artifacts are preserved.
