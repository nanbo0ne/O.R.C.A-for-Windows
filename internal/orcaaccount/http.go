package orcaaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

const maxResponse = 1 << 20

func (c *Client) formRequest(ctx context.Context, path string, values url.Values, out any) error {
	return c.send(ctx, http.MethodPost, path, "", "application/x-www-form-urlencoded", []byte(values.Encode()), out)
}

func (c *Client) jsonRequest(ctx context.Context, method, path, token string, body, out any) error {
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			return ErrInvalid
		}
	}
	return c.send(ctx, method, path, token, "application/json", b, out)
}

func (c *Client) send(ctx context.Context, method, path, token, contentType string, body []byte, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, method, c.issuer+APIPrefix+path, bytes.NewReader(body))
	if err != nil {
		return ErrInvalid
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Cache-Control", "no-store")
	if body != nil {
		r.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrNetwork
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrNetwork
	}
	defer clear(b)
	if len(b) > maxResponse {
		return ErrProtocol
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &failure)
		if resp.StatusCode == 401 || (path == "/token" && failure.Error == "invalid_grant") {
			return ErrUnauthenticated
		}
		if resp.StatusCode == 403 {
			return ErrAuthorization
		}
		if resp.StatusCode == 400 {
			return ErrInvalid
		}
		return ErrNetwork
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(b, out) != nil {
		return ErrProtocol
	}
	return nil
}

func (c *Client) authorized(ctx context.Context, method, path string, body, out any) (uint64, error) {
	c.mu.Lock()
	gen, life := c.generation, c.lifetime
	c.mu.Unlock()
	if binding, ok := ctx.Value(generationContextKey{}).(generationBinding); ok && (binding.client != c || binding.gen != gen) {
		return gen, ErrStale
	}
	ctx, cancel := linkContext(ctx, life)
	defer cancel()
	token, err := c.access(ctx, gen, false, "")
	if err != nil {
		return gen, err
	}
	err = c.jsonRequest(ctx, method, path, token, body, out)
	if errors.Is(err, ErrUnauthenticated) {
		// Only safe reads are retried. Never replay a credential-minting POST.
		if method != http.MethodGet {
			return gen, c.invalidate(gen)
		}
		token, err = c.access(ctx, gen, true, token)
		if err != nil {
			// The refresh worker already invalidated its generation on failure.
			return gen, err
		}
		err = c.jsonRequest(ctx, method, path, token, body, out)
		if errors.Is(err, ErrUnauthenticated) {
			return gen, c.invalidate(gen)
		}
	}
	if stale := c.check(gen); stale != nil {
		return gen, stale
	}
	if ctx.Err() != nil {
		return gen, ctx.Err()
	}
	return gen, err
}
