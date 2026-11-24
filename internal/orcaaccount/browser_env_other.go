//go:build !windows

package orcaaccount

// systemBrowserEnvironment returns nil on platforms where the isolated beta
// profile does not rewrite the variables a browser uses to find its data.
func systemBrowserEnvironment() []string { return nil }
