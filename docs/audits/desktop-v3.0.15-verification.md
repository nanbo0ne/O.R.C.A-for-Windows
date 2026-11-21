# O.R.C.A. Desktop 3.0.15 Verification

## Scope and privacy

This release adds WhaleStack account sign-in to the provider settings and fixes the Linux sandbox tests that blocked the release gate. Checks use synthetic credentials against the login service; no real account password, personal conversation, model configuration or service log was read or recorded. The relay key returned by the login service is never written to any document.

## Changes under test

- `WhaleStackSignIn` posts the entered account to `POST /api/app/login`, decodes `{email, name, baseUrl, apiKey, keyName}`, reads the model list from `{baseUrl}/models`, and only then registers the provider and writes the key, so a half-configured provider never appears.
- The login service derives one relay key per account (`orca` + 28 hex characters), creates it once as `ORCA 桌面端` through `POST /api/v1/api-keys` with `custom_key`, and reuses it on later sign-ins.
- The card reports the login service's own reason (bad password, no model access, busy service) inline; `apply()` does not rethrow, so the card captures the message itself.
- Signing in again replaces the existing `鲸栈` entry instead of appending a second one.
- `internal/sandbox` tests now expect bubblewrap wrapping on Linux when `bwrap` is on PATH and compare the executable by base name, since `exec.LookPath` returns an absolute path.

## Local evidence

- Login service `node --test tests/`: 22 tests passed, including stable relay key reuse, rejection paths and the unconfigured-relay case.
- Desktop `go build ./...` and `go test . -run 'Provider|Settings|Whale'`: passed.
- `internal/sandbox`: passed on Windows (unwrapped path); the Linux path is covered by the release workflow's runner.
- Frontend `npm run typecheck`: passed. `wails generate module` produced `WhaleStackSignIn` in the bindings.
- `desktop-v3.0.15` CI run: cache guard passes; platform builds are recorded by the release workflow.

## Acceptance boundary

No production account sign-in was exercised on a user's personal computer, and no real credential was submitted from the release machine. Windows remains unsigned unless the dispatch used the explicit unsigned override; macOS is not notarized. Minisign covers update integrity only.
