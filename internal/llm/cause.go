package llm

import (
	"errors"
	"net/http"
	"strings"
)

// Why a model failed, as far as the user can do something about it
// (docs/spec/models.md, When a model fails).
const (
	// CauseKey: the server refuses the key, or it is missing.
	CauseKey = "key"
	// CauseUnreachable: no answer from the server at all.
	CauseUnreachable = "unreachable"
)

// Cause names why a model call failed: CauseKey, CauseUnreachable, or ""
// for anything else. The caller makes sure the turn itself was not ended.
func Cause(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
			return CauseKey
		case apiErr.StatusCode == http.StatusBadRequest && namesTheKey(apiErr.Body):
			// Gemini answers a missing key with 400 "Missing or invalid
			// Authorization header" (seen 2026-11-05).
			return CauseKey
		}
		return ""
	}
	// No HTTP answer: refused, unresolved, broken off or out of time.
	return CauseUnreachable
}

func namesTheKey(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "authorization") || strings.Contains(b, "api key") || strings.Contains(b, "api_key")
}
