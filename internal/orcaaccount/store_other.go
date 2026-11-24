//go:build !windows

package orcaaccount

// NewSystemStore deliberately provides no plaintext fallback on other systems.
func NewSystemStore(string) (Store, error) { return nil, ErrUnsupported }
