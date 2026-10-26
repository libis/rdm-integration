// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

// Package reauth carries "the user must re-authenticate at the OAuth
// provider" signals from plugin API clients to the HTTP boundary, where they
// become structured 401 responses the frontend turns into a new login with
// extra scopes and/or a required identity domain.
package reauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

const marker = "*reauth*"

const safeFailureMessage = "operation failed"

var (
	safeScope  = regexp.MustCompile(`^urn:globus:auth:scope:[A-Za-z0-9][A-Za-z0-9:./*\[\]@+_-]{0,511}$`)
	safeDomain = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
)

// Error describes what the new login must include.
type Error struct {
	RequiredScopes  []string `json:"required_scopes,omitempty"`
	RequiredDomains []string `json:"required_domains,omitempty"`
	Message         string   `json:"message,omitempty"`
}

// Error serializes the value between markers so it survives string
// round-trips (Redis-cached job errors, fmt.Errorf wrapping without %w).
func (e *Error) Error() string {
	b, _ := json.Marshal(Sanitize(e))
	return marker + string(b) + marker
}

// Sanitize keeps only the Globus reauthorization metadata the frontend needs.
// Provider-supplied messages are intentionally discarded because they can
// echo credentials or other request data.
func Sanitize(e *Error) *Error {
	if e == nil {
		return nil
	}
	safe := &Error{}
	for _, scope := range e.RequiredScopes {
		if safeScope.MatchString(scope) {
			safe.RequiredScopes = append(safe.RequiredScopes, scope)
		}
		if len(safe.RequiredScopes) == 32 {
			break
		}
	}
	for _, domain := range e.RequiredDomains {
		domain = strings.TrimSpace(domain)
		if safeDomain.MatchString(domain) {
			safe.RequiredDomains = append(safe.RequiredDomains, domain)
		}
		if len(safe.RequiredDomains) == 32 {
			break
		}
	}
	return safe
}

// Parse extracts a marker-embedded Error from s. Returns nil when no
// well-formed, actionable (scopes or domains present) marker is found.
func Parse(s string) *Error {
	first := strings.Index(s, marker)
	if first < 0 {
		return nil
	}
	rest := s[first+len(marker):]
	end := strings.Index(rest, marker)
	if end < 0 {
		return nil
	}
	res := &Error{}
	if err := json.Unmarshal([]byte(rest[:end]), res); err != nil {
		return nil
	}
	res = Sanitize(res)
	if len(res.RequiredScopes) == 0 && len(res.RequiredDomains) == 0 {
		return nil
	}
	return res
}

// FromError returns the reauth Error carried by err — typed in the wrap
// chain, or embedded as a marker in its text. Nil when err carries none.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		safe := Sanitize(typed)
		if len(safe.RequiredScopes) == 0 && len(safe.RequiredDomains) == 0 {
			return nil
		}
		return safe
	}
	return Parse(err.Error())
}

// CachedError returns a safe job error string. It preserves only validated
// reauthorization metadata; all other failures become a fixed message.
func CachedError(err error) string {
	if reauthErr := FromError(err); reauthErr != nil {
		return reauthErr.Error()
	}
	return safeFailureMessage
}

// WriteError writes validated reauthorization metadata for reauth errors.
// Every other error gets a fixed message so details cannot expose secrets.
func WriteError(w http.ResponseWriter, err error) {
	if reauthErr := FromError(err); reauthErr != nil {
		b, _ := json.Marshal(struct {
			Reauth *Error `json:"reauth"`
		}{Reauth: Sanitize(reauthErr)})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(b)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("500 - request failed"))
}
