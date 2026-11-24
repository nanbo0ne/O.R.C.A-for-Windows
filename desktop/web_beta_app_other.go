//go:build !windows

package main

func closeWebBeta()                         {}
func startWebBetaBackground(a *App)         {}
func ensureWebBetaModelEntry() error        { return nil }
func (a *App) openWebBetaBackground() error { return nil }
