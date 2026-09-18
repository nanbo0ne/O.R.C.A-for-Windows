//go:build !windows

// Package installipc provides Windows installer shutdown signalling.
package installipc

import "context"

// RequestShutdown is a no-op on non-Windows platforms.
func RequestShutdown(string) (bool, error) { return false, nil }

// Listen is a no-op on non-Windows platforms.
func Listen(context.Context, string, func()) (func(), error) { return func() {}, nil }
