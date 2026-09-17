package openai

import (
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// NormalizeBaseURL accepts an API base or a full Chat Completions/Responses URL.
// Only known terminal paths are stripped; proxy prefixes and escaped paths stay
// intact. This client still uses Chat Completions, not the Responses protocol.
func NormalizeBaseURL(raw string) (string, error) {
	u, err := provider.ParseEndpointURL(raw)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch {
	case strings.HasSuffix(path, "/api/v1/chat"):
		return "", fmt.Errorf("selected OpenAI-compatible Chat Completions, but base_url points to native /api/v1/chat; use the server's OpenAI-compatible base_url (typically /v1), which serves /v1/chat/completions")
	case strings.HasSuffix(path, "/v1/messages"):
		return "", fmt.Errorf("selected OpenAI-compatible Chat Completions, but base_url points to Anthropic /v1/messages; choose kind=\"anthropic\" for Messages or use the server's OpenAI-compatible base_url")
	}
	for _, suffix := range []string{"/chat/completions", "/responses"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	u.Path, u.RawPath = "", ""
	return u.String() + path, nil
}
