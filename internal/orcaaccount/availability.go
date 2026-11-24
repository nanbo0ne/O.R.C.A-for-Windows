package orcaaccount

import (
	"context"
	"net/http"
	"time"
)

// CheckDesktopService probes only the desktop session route, without tokens or
// cookies. A normal unauthenticated response establishes route availability,
// not successful login, model access or billing readiness.
func (c *Client) CheckDesktopService(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.issuer+APIPrefix+"/session", nil)
	if err != nil {
		return ErrInvalid
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Cache-Control", "no-store")
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrNetwork
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil
	case http.StatusNotFound, http.StatusGone, http.StatusNotImplemented:
		return ErrServiceUnavailable
	case http.StatusForbidden:
		return ErrAuthorization
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return ErrNetwork
	default:
		return ErrProtocol
	}
}
