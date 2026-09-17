package provider

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseEndpointURL validates URLs before callers append API paths. Do not echo
// the raw URL on errors: userinfo, query strings, or fragments may contain keys.
func ParseEndpointURL(raw string) (*url.URL, error) {
	u, err := ParseRequestURL(raw)
	if err != nil {
		return nil, err
	}
	if u.RawQuery != "" || u.ForceQuery {
		return nil, fmt.Errorf("endpoint must not contain query parameters; configure the API base_url without a query string")
	}
	return u, nil
}

// ParseRequestURL validates a complete request URL. Explicit catalog URLs may
// contain filters or proxy routing queries; unlike API bases no path is appended.
func ParseRequestURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("endpoint must be a valid absolute HTTP(S) URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return nil, fmt.Errorf("endpoint must be an absolute HTTP(S) URL with a host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("endpoint must not contain userinfo; configure authentication using the API key setting")
	}
	if strings.Contains(raw, "#") {
		return nil, fmt.Errorf("endpoint must not contain a fragment; configure the API base_url without a fragment")
	}
	return u, nil
}
