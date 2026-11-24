package orcaaccount

import (
	"context"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenSystemBrowser invokes the OS URL handler without a command shell. It only
// accepts this client's fixed issuer authorize endpoint, never arbitrary input.
func OpenSystemBrowser(ctx context.Context, rawURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host != "sub.aichat.diy" || u.User != nil || u.Path != APIPrefix+"/authorize" || u.Fragment != "" {
		return ErrInvalid
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	// Hand the handler the user's real profile paths. Without this the isolated
	// beta environment makes the browser resolve a different user-data directory,
	// so it cannot join the window the user already has open and starts a second,
	// signed-out one.
	if restored := systemBrowserEnvironment(); len(restored) > 0 {
		cmd.Env = restored
	}
	if cmd.Start() != nil {
		return ErrBrowser
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
