//go:build windows

package webbetahost

import "strings"

// Login preview installs no adapter, probes, uploads or request dispatch API,
// even in an acceptance build with diagnostic environment flags set.
func documentScripts(loginOnly bool, generation string, diagnostics, probe bool) []string {
	if loginOnly {
		return []string{`(() => { const report = () => { const present = !!document.querySelector('input[type="password"],input[autocomplete="username"],input[type="tel"]'); window.chrome.webview.postMessage(JSON.stringify({kind:'login_health',generation:'` + generation + `',code:present?'login_controls_present':'document_loaded'})); }; report(); setTimeout(report, 2000); })()`}
	}
	var scripts []string
	if diagnostics && probe {
		scripts = append(scripts, strings.ReplaceAll(probeScript, "__ORCA_DOCUMENT_ID__", generation))
	}
	site := siteScript
	if diagnostics {
		site = strings.ReplaceAll(site, "__ORCA_DIAGNOSTICS__", "enabled")
	}
	return append(scripts, site, rawResponseScript, strings.ReplaceAll(pageScript, "__ORCA_DOCUMENT_ID__", generation))
}
