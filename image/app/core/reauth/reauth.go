// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

// Package reauth carries "the user must re-authenticate at the OAuth
// provider" signals from plugin API clients to the HTTP boundary, where they
// become structured 401 responses the frontend turns into a new login with
// extra scopes and/or a required identity domain.
package reauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const marker = "*reauth*"

// Error describes what the new login must include.
type Error struct {
	RequiredScopes  []string `json:"required_scopes,omitempty"`
	RequiredDomains []string `json:"required_domains,omitempty"`
	Message         string   `json:"message,omitempty"`
}

// Error serializes the value between markers so it survives string
// round-trips (Redis-cached job errors, fmt.Errorf wrapping without %w).
func (e *Error) Error() string {
	b, _ := json.Marshal(e)
	return marker + string(b) + marker
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
		return typed
	}
	return Parse(err.Error())
}

// WriteError writes err to w. Reauth errors become 401 with a JSON body
// {"reauth": {...}}; all other errors keep the legacy "500 - %v" text.
func WriteError(w http.ResponseWriter, err error) {
	if reauthErr := FromError(err); reauthErr != nil {
		b, _ := json.Marshal(struct {
			Reauth *Error `json:"reauth"`
		}{Reauth: reauthErr})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(b)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(fmt.Sprintf("500 - %v", err)))
}
