package webbetahost

import (
	"errors"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

var (
	ErrRecheckBusy        = errors.New("web_recheck_busy")
	ErrRecheckTimeout     = errors.New("web_recheck_timeout")
	ErrRecheckUnavailable = errors.New("web_recheck_unavailable")
	ErrRecheckIdentity    = errors.New("web_recheck_identity")
)

// Only fixed categories cross the desktop bridge, never page text or URLs.
func recheckStateError(state webbeta.BrowserState) error {
	switch state.Blocked {
	case "signed_out":
		return errors.New("web_recheck_signed_out")
	case "challenge", "restricted", "rate_limited":
		return errors.New("web_recheck_" + state.Blocked)
	case "adapter_not_verified":
		return errors.New("web_recheck_adapter")
	case "browser_process_failed", "identity_closed", "external_navigation":
		return ErrRecheckUnavailable
	}
	if state.Busy {
		return ErrRecheckBusy
	}
	if !state.Ready || state.Blocked != "" {
		return errors.New("web_recheck_not_ready")
	}
	return nil
}
