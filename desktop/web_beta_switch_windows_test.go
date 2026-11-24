//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

type switchCheckBrowser struct {
	lifecycleWebBrowser
	checkError error
}

func (b *switchCheckBrowser) Recheck(context.Context) error { return b.checkError }

func TestWebBetaAccountSwitchRetriesFailedInitializationAndRecheck(t *testing.T) {
	for _, stage := range []string{"initialization", "recheck"} {
		t.Run(stage, func(t *testing.T) {
			isolatedWebRuntime(t)
			old, failed, fresh := &switchCheckBrowser{}, &switchCheckBrowser{}, &switchCheckBrowser{}
			calls := 0
			newWebBetaBrowser = func(string, string) (webBetaBrowser, error) {
				calls++
				switch calls {
				case 1:
					return old, nil
				case 2:
					if stage == "initialization" {
						return failed, errors.New("synthetic initialization failure")
					}
					failed.checkError = errors.New("web_recheck_timeout")
					return failed, nil
				default:
					return fresh, nil
				}
			}
			a := &App{}
			s, err := a.WebBetaOpen()
			if err != nil {
				t.Fatal(err)
			}
			s.Settings.Enabled = true
			if err := a.WebBetaConfigure(s.Settings); err != nil {
				t.Fatal(err)
			}
			if err := a.webBetaUseCurrentLogin(context.Background()); err == nil {
				t.Fatal("failed switch reported success")
			}
			s = a.WebBetaStatus()
			if !s.SendAuthorized || s.Settings.Enabled || s.Blocked != webbeta.IdentitySwitchPending || !webBetaAdmission.blocked {
				t.Fatal("failed switch lost authorization or reopened admission")
			}
			if err := a.WebBetaRecover(); err == nil {
				t.Fatal("generic recovery bypassed failed switch")
			}
			if err := a.webBetaUseCurrentLogin(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !failed.released || !a.WebBetaStatus().Settings.Enabled || webBetaAdmission.blocked {
				t.Fatal("retry did not drain and restore")
			}
			if old.sends+failed.sends+fresh.sends != 0 {
				t.Fatal("account recovery sent a message")
			}
		})
	}
}

func TestWebBetaAccountSwitchPreservesProfileAndGrant(t *testing.T) {
	isolatedWebRuntime(t)
	old, next := &lifecycleWebBrowser{}, &lifecycleWebBrowser{}
	calls := 0
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) {
		calls++
		if calls == 1 {
			return old, nil
		}
		return next, nil
	}
	a := &App{}
	status, err := a.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	settings := status.Settings
	settings.Enabled = true
	if err := a.WebBetaConfigure(settings); err != nil {
		t.Fatal(err)
	}
	p, err := webBetaProfile()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(p.BrowserPath(), "synthetic-login-marker")
	if err := os.MkdirAll(p.BrowserPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("retained-login"), 0600); err != nil {
		t.Fatal(err)
	}
	before := a.WebBetaStatus().Generation
	if err := a.webBetaUseCurrentLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "retained-login" {
		t.Fatal("profile cleared", err)
	}
	status = a.WebBetaStatus()
	if !old.released || calls != 2 || !status.SendAuthorized || !status.Settings.Enabled || status.Generation == before || webBetaAdmission.blocked || status.Blocked != "" {
		t.Fatalf("switch incomplete: %+v", status)
	}
	if old.sends != 0 || next.sends != 0 {
		t.Fatal("switch sent model content")
	}
}

func TestWebBetaAccountSwitchFailedNativeReleaseIsRetryable(t *testing.T) {
	isolatedWebRuntime(t)
	old := &lifecycleWebBrowser{releaseErr: errors.New("synthetic native release failure")}
	next := &lifecycleWebBrowser{}
	calls := 0
	newWebBetaBrowser = func(string, string) (webBetaBrowser, error) {
		calls++
		if calls == 1 {
			return old, nil
		}
		return next, nil
	}
	a := &App{}
	status, err := a.WebBetaOpen()
	if err != nil {
		t.Fatal(err)
	}
	settings := status.Settings
	settings.Enabled = true
	if err := a.WebBetaConfigure(settings); err != nil {
		t.Fatal(err)
	}
	if err := a.webBetaUseCurrentLogin(context.Background()); err == nil {
		t.Fatal("failed native release accepted")
	}
	if !webBetaAdmission.blocked || calls != 1 || a.WebBetaStatus().Blocked != webbeta.IdentitySwitchPending || !a.WebBetaStatus().SendAuthorized {
		t.Fatal("failed switch reopened or revoked grant")
	}
	old.releaseErr = nil
	if err := a.webBetaUseCurrentLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if webBetaAdmission.blocked || !a.WebBetaStatus().Settings.Enabled || calls != 2 {
		t.Fatal("retry did not complete")
	}
}
