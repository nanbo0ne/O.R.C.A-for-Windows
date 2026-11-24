//go:build windows

package orcaaccount

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// systemBrowserEnvironment restores the user's real profile paths for the URL
// handler. ORCA beta runs with APPDATA, LOCALAPPDATA, USERPROFILE and HOME
// pointed at its isolated data directory. A browser started with those values
// resolves a different user-data directory, cannot join the instance the user
// already has open, and opens a second window with none of that session's
// cookies, so the account looks signed out. Volatile Environment holds the
// values the system assigned at logon, before any process rewrote them.
func systemBrowserEnvironment() []string {
	env := os.Environ()
	real := map[string]string{}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Volatile Environment`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	for _, name := range []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE", "HOMEDRIVE", "HOMEPATH"} {
		value, _, err := key.GetStringValue(name)
		if err == nil && value != "" {
			real[name] = value
		}
	}
	if len(real) == 0 {
		return nil
	}
	if drive, ok := real["HOMEDRIVE"]; ok {
		if path, ok := real["HOMEPATH"]; ok {
			real["HOME"] = drive + path
		}
	}
	out := make([]string, 0, len(env)+len(real))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if value, ok := real[strings.ToUpper(name)]; ok {
			out = append(out, name+"="+value)
			continue
		}
		out = append(out, entry)
	}
	return out
}
