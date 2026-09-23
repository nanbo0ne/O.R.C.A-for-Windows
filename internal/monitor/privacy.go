package monitor

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

var dataURI = regexp.MustCompile(`(?i)data:[^\s"']+`)
var bearer = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`)
var knownKey = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{12,}|gh[pousr]_[A-Za-z0-9_]{16,}|github_pat_[A-Za-z0-9_]{16,}|AIza[A-Za-z0-9_-]{20,})`)
var assignedSecret = regexp.MustCompile(`(?i)\b(?:api[_-]?key|(?:access[_-]?|refresh[_-]?)?token|authorization|cookie|password|secret)["']?\s*(?:=|:)\s*(?:"(?:\\.|[^"\\])*"?|'(?:\\.|[^'\\])*'?|[^\s"',;}]+)`)

func Text(s string) (string, bool) {
	// A whole structured fragment can be filtered by key, including encoded JSON
	// nested in tool arguments/results. Never reset the sanitizer's depth budget.
	trimmed := strings.TrimSpace(s)
	if len(s) <= 16384 && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`)) {
		var value any
		if json.Unmarshal([]byte(s), &value) == nil {
			partial := false
			clean := sanitize(value, "", 0, &partial)
			if partial {
				out, _ := json.Marshal(clean)
				return string(out), true
			}
			return s, false
		}
	}
	return textLiteral(s)
}

func textLiteral(s string) (string, bool) {
	partial := false
	if len(s) > 16384 {
		n := 16384
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n]
		partial = true
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
		partial = true
	}
	t := dataURI.ReplaceAllString(s, "[data omitted]")
	t = bearer.ReplaceAllString(t, "Bearer [redacted]")
	t = knownKey.ReplaceAllString(t, "[key redacted]")
	t = assignedSecret.ReplaceAllString(t, "[credential assignment redacted]")
	return t, partial || t != s
}

func SanitizeJSON(body []byte) (json.RawMessage, bool) {
	var value any
	if len(body) > MaxBody || json.Unmarshal(body, &value) != nil {
		return json.RawMessage(`{"omitted":"invalid or oversized JSON"}`), true
	}
	partial := false
	value = sanitize(value, "", 0, &partial)
	out, err := json.Marshal(value)
	if err != nil || len(out) > MaxBody {
		return json.RawMessage(`{"omitted":"sanitized payload limit"}`), true
	}
	return out, partial
}

func privateKey(key string) bool {
	k := strings.ReplaceAll(strings.ToLower(key), "-", "_")
	return strings.Contains(k, "authorization") || strings.Contains(k, "api_key") || strings.Contains(k, "apikey") || strings.Contains(k, "secret") || strings.Contains(k, "password") || k == "token" || k == "access_token" || k == "refresh_token" || k == "cookie" || k == "signature" || k == "reasoning_content" || k == "thinking" || k == "image_url" || k == "image" || k == "data"
}

func sanitize(v any, key string, depth int, partial *bool) any {
	if depth > 24 {
		*partial = true
		return "[depth limit]"
	}
	// Signed/original reasoning replay is withheld so PostLLMCall cannot be bypassed.
	if privateKey(key) {
		_, thinkingConfig := v.(map[string]any)
		if !strings.EqualFold(key, "thinking") || !thinkingConfig {
			*partial = true
			return "[withheld]"
		}
	}
	switch x := v.(type) {
	case map[string]any:
		if x["type"] == "image" || x["type"] == "image_url" || x["type"] == "base64" || x["type"] == "thinking" || x["type"] == "redacted_thinking" {
			*partial = true
			return map[string]any{"type": x["type"], "omitted": true}
		}
		for k, v := range x {
			x[k] = sanitize(v, k, depth+1, partial)
		}
		return x
	case []any:
		if len(x) > 1024 {
			x = x[:1024]
			*partial = true
		}
		for i, v := range x {
			x[i] = sanitize(v, "", depth+1, partial)
		}
		return x
	case string:
		if len(x) <= 16384 && (strings.HasPrefix(strings.TrimSpace(x), "{") || strings.HasPrefix(strings.TrimSpace(x), "[") || strings.HasPrefix(strings.TrimSpace(x), `"`)) {
			var nested any
			if json.Unmarshal([]byte(x), &nested) == nil {
				out, _ := json.Marshal(sanitize(nested, "", depth+1, partial))
				return string(out)
			}
		}
		t, p := textLiteral(x)
		*partial = *partial || p
		return t
	default:
		return x
	}
}
