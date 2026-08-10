// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package globus

import (
	"encoding/json"
	"fmt"
	"integration/app/core/reauth"
	"regexp"
	"strings"
)

type authorizationParameters struct {
	SessionRequiredSingleDomain json.RawMessage `json:"session_required_single_domain"`
	SessionMessage              string          `json:"session_message"`
}

type errorBody struct {
	Code                    string                   `json:"code"`
	Message                 string                   `json:"message"`
	RequiredScopes          []string                 `json:"required_scopes"`
	AuthorizationParameters *authorizationParameters `json:"authorization_parameters"`
}

// Matches session_required_single_domain fragments embedded in GridFTP 530
// message text (e.g. after "530-GridFTP-JSON-Result: {...}"). Captures either
// the JSON array or the JSON string form of the value.
var embeddedDomainsR = regexp.MustCompile(`"session_required_single_domain"\s*:\s*(\[[^\]]*\]|"[^"]*")`)

// interpretGlobusError turns a non-2xx Globus API response body into an
// error. ConsentRequired and session/domain-policy errors become
// *reauth.Error so the HTTP boundary can trigger re-authentication.
//
// All other API errors keep the exact "Code: Message" text form — the
// folder-landing candidate loop in options.go classifies errors by that
// string (isNotFoundError); do not change the format.
func interpretGlobusError(statusCode int, body []byte) error {
	parsed := errorBody{}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Code == "" {
		return fmt.Errorf("globus request failed: %d - %s", statusCode, string(body))
	}
	if parsed.Code == "ConsentRequired" && len(parsed.RequiredScopes) > 0 {
		return &reauth.Error{RequiredScopes: parsed.RequiredScopes, Message: parsed.Message}
	}
	if parsed.AuthorizationParameters != nil {
		if domains := decodeDomains(parsed.AuthorizationParameters.SessionRequiredSingleDomain); len(domains) > 0 {
			message := parsed.AuthorizationParameters.SessionMessage
			if message == "" {
				message = parsed.Message
			}
			return &reauth.Error{RequiredDomains: domains, Message: message}
		}
	}
	if domains := embeddedDomains(parsed.Message); len(domains) > 0 {
		return &reauth.Error{RequiredDomains: domains, Message: parsed.Message}
	}
	return fmt.Errorf("%v: %v", parsed.Code, parsed.Message)
}

// decodeDomains accepts both JSON forms Globus uses for
// session_required_single_domain: an array of strings or a single, possibly
// comma-separated, string.
func decodeDomains(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	arr := []string{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return cleanDomains(arr)
	}
	s := ""
	if err := json.Unmarshal(raw, &s); err == nil {
		return cleanDomains(strings.Split(s, ","))
	}
	return nil
}

func cleanDomains(in []string) []string {
	out := []string{}
	for _, d := range in {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func embeddedDomains(message string) []string {
	m := embeddedDomainsR.FindStringSubmatch(message)
	if len(m) < 2 {
		return nil
	}
	return decodeDomains(json.RawMessage(m[1]))
}
