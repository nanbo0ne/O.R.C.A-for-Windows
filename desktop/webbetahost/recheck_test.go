package webbetahost

import (
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func TestRecheckStateError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state webbeta.BrowserState
		want  string
	}{
		{"ready", webbeta.BrowserState{Ready: true}, ""},
		{"busy", webbeta.BrowserState{Ready: true, Busy: true}, "web_recheck_busy"},
		{"loading", webbeta.BrowserState{}, "web_recheck_not_ready"},
		{"login", webbeta.BrowserState{Blocked: "signed_out"}, "web_recheck_signed_out"},
		{"challenge", webbeta.BrowserState{Blocked: "challenge"}, "web_recheck_challenge"},
		{"restricted", webbeta.BrowserState{Blocked: "restricted"}, "web_recheck_restricted"},
		{"rate", webbeta.BrowserState{Blocked: "rate_limited"}, "web_recheck_rate_limited"},
		{"adapter", webbeta.BrowserState{Blocked: "adapter_not_verified"}, "web_recheck_adapter"},
		{"crash", webbeta.BrowserState{Blocked: "browser_process_failed"}, "web_recheck_unavailable"},
		{"unknown-private", webbeta.BrowserState{Ready: true, Blocked: "SYNTHETIC_PRIVATE_ERROR", URL: "https://private.invalid/?secret=synthetic"}, "web_recheck_not_ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := recheckStateError(tc.state)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}
