# Native Host Revalidation

User-authorized follow-up after the beta browser profile/transfer confinement
changes. This covers the generic browser host, not authenticated DeepSeek Web,
the full production UI, or a human account login.

## Result

PASS, one complete fresh-fixture run with the beta isolated-profile policy active:

| Existing native suite | Time |
| --- | ---: |
| Adversarial action boundaries | 2.65s |
| Upload interception | 1.12s |
| Download transfers | 61.86s |
| CDP callback lifecycle | 1.04s |
| Wrapper total | 66.68s |
| Go package total | 67.456s |

Coverage includes iframe/closed-shadow/protected-input rejection and a positive
plain-descendant control; upload ownership, explicitly approved snapshots,
mutation/observation revocation, multiple selection, cancellation, cross-process
frame denial and close revocation; denied download without publication, approved
payload hash, cancellation, approval timeout and close; late native callback
completion and final callback reclamation. Existing assertions were unchanged.

Passing evidence:
`.tmp/native-host-beta-suite-cc2bb3776ea849f3a93cd28944a629ab.jsonl`

SHA256:
`000dd7d2066808b79567d569bf9383da0021658c36cc4b385f7b8462a16a57c6`

Fresh launcher fixture: `D:/AI-Reasonix/.tmp/nh-a3e180cc`. All test-created
profiles and snapshots were removed by the test cleanup. The final temp-root
listing contained only the four synthetic environment directories APPDATA, HOME,
LOCALAPPDATA and USERPROFILE, not a retained WebView profile.

## Harness and Command

Only new test file:
`desktop/browserhost/native_beta_revalidation_windows_test.go`.
The wrapper invokes the existing native tests, enables
`config.SetIsolatedProfileHome`, refuses a reused/nonempty root, checks fixture
paths before running, clears inherited WEBVIEW2 overrides, and stops after a
failed suite. It requires both native opt-in flags. No production code changed
in this follow-up.

Run from `desktop`, with native-queue approval and an elevated/unsandboxed test
process (not a visible application or interactive desktop helper):

```powershell
$env:GOCACHE='D:/AI-Reasonix/.tmp/orca-beta-gocache'
$env:GOPROXY='off'
$env:GOTMPDIR='D:/AI-Reasonix/.tmp/orca-beta-test-tmp'
$env:GOPATH='C:/Users/16260/go'
$fixture='D:/AI-Reasonix/.tmp/nh-'+[guid]::NewGuid().ToString('N').Substring(0,8)
foreach($dir in @('home','roaming','local','temp')) {
  [System.IO.Directory]::CreateDirectory((Join-Path $fixture $dir)) | Out-Null
}
$env:HOME=Join-Path $fixture 'home'
$env:USERPROFILE=$env:HOME
$env:APPDATA=Join-Path $fixture 'roaming'
$env:LOCALAPPDATA=Join-Path $fixture 'local'
$env:TEMP=Join-Path $fixture 'temp'
$env:TMP=$env:TEMP
$env:ORCA_BROWSER_NATIVE_BETA_ROOT=$env:TEMP
$env:ORCA_BROWSER_NATIVE_BETA_TEST='1'
$env:ORCA_BROWSER_NATIVE_TEST='1'
$env:ORCA_BROWSER_TRANSFER_TRACE=''
go test -mod=readonly ./browserhost -run '^TestNativeBetaHostRevalidation$' -count=1 -timeout=240s -json
```

The wrapper aligns test-process GOTMPDIR with the fresh TEMP root because this
Go version's `testing.T.TempDir` uses GOTMPDIR. The compiler's temporary root
remains separate before the test starts. Each native suite creates a fresh
profile; no cookies are reused. All page traffic is to synthetic loopback
fixtures. No protected logs, personal configuration, personal cookies, model
endpoints or CUA were accessed. All environment changes are process-local.

## Earlier Attempts

- Sandboxed launch failed before WebView creation completed; cleanup reported a
  locked synthetic metrics file. It was not acceptance success. After that test
  process exited, only its exact leftover fixture directory was checked and
  removed. Evidence: `native-host-beta-revalidation-4282b49b3f1a4e1c94b0b4a9e291f387.jsonl`.
- The elevated adversarial precheck passed in 2.55s. Evidence:
  `native-host-beta-adversarial-6c073aaf6103453d81a8aed4d494deac.jsonl`.
- Two wrapper integration attempts correctly rejected upload profiles outside
  the isolated root because GOTMPDIR had not been aligned. No production path
  guard or assertion was weakened. Evidence: `native-host-beta-suite-87ade4d6f9ab4a0f978ac9ebf6e79df3.jsonl`
  and `native-host-beta-suite-5bde3741d6c34387ac8738bf2c3ddd3a.jsonl`.

All evidence files above are under the worktree's `.tmp/` directory. Earlier
failed attempts remain recorded separately from the final passing result.

## Remaining Gates

Full beta production-UI revalidation after the profile change remains pending.
Human login, authenticated DeepSeek readiness/sends, identity switching and
real-model tool/vision acceptance remain deferred. This run is not a real Wails
cold-start measurement, a real-account test, or release/deployment approval.
The native queue is free after process completion.
