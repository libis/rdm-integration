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

// maxScopeLength leaves room for a transfer scope with several collection
// dependencies; one data_access dependency is about 75 characters.
const maxScopeLength = 2048

var (
	safeScope  = regexp.MustCompile(`^urn:globus:auth:scope:[A-Za-z0-9][A-Za-z0-9:./*\[\] @+_-]*$`)
	safeDomain = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	// Globus policy and identity ids are UUIDs
	safeID = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// isSafeScope accepts Globus scope strings: Globus separates the dependencies
// inside brackets with single spaces (one data_access dependency per
// collection), so spaces are allowed there and nowhere else, and the brackets
// must balance.
func isSafeScope(scope string) bool {
	if len(scope) > maxScopeLength || !safeScope.MatchString(scope) {
		return false
	}
	depth := 0
	prev := rune(0)
	for _, c := range scope {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 || prev == ' ' {
				return false
			}
		case ' ':
			if depth == 0 || prev == ' ' || prev == '[' {
				return false
			}
		}
		prev = c
	}
	return depth == 0
}

// Error describes what the new login must include.
type Error struct {
	RequiredScopes     []string `json:"required_scopes,omitempty"`
	RequiredDomains    []string `json:"required_domains,omitempty"`
	RequiredPolicies   []string `json:"required_policies,omitempty"`
	RequiredIdentities []string `json:"required_identities,omitempty"`
	RequiredMFA        bool     `json:"required_mfa,omitempty"`
	Message            string   `json:"message,omitempty"`
}

// Actionable reports whether a new login has something to satisfy.
func (e *Error) Actionable() bool {
	return e != nil && (len(e.RequiredScopes) > 0 || len(e.RequiredDomains) > 0 ||
		len(e.RequiredPolicies) > 0 || len(e.RequiredIdentities) > 0 || e.RequiredMFA)
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
		if isSafeScope(scope) {
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
	safe.RequiredPolicies = safeIDs(e.RequiredPolicies)
	safe.RequiredIdentities = safeIDs(e.RequiredIdentities)
	safe.RequiredMFA = e.RequiredMFA
	return safe
}

func safeIDs(ids []string) []string {
	var res []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); safeID.MatchString(id) {
			res = append(res, id)
		}
		if len(res) == 32 {
			break
		}
	}
	return res
}

// Parse extracts a marker-embedded Error from s. Returns nil when no
// well-formed, actionable marker is found.
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
	if !res.Actionable() {
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
		if !safe.Actionable() {
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
