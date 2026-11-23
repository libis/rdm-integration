// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package globus

import (
	"encoding/json"
	"errors"
	"fmt"
	"integration/app/core/reauth"
	"regexp"
	"strings"
)

// authorizationParameters is the Globus Auth Requirements Error part that
// says what the next login must satisfy. Globus Auth cannot combine policies
// with the other session requirements; the frontend handles that when it
// builds the login URL.
type authorizationParameters struct {
	RequiredScopes              []string        `json:"required_scopes"`
	SessionRequiredSingleDomain json.RawMessage `json:"session_required_single_domain"`
	SessionRequiredPolicies     json.RawMessage `json:"session_required_policies"`
	SessionRequiredIdentities   json.RawMessage `json:"session_required_identities"`
	SessionRequiredMFA          bool            `json:"session_required_mfa"`
	SessionMessage              string          `json:"session_message"`
}

// sessionRequirements returns the session demands as a reauth error, nil when
// there are none.
func (p *authorizationParameters) sessionRequirements() *reauth.Error {
	if p == nil {
		return nil
	}
	e := &reauth.Error{
		RequiredDomains:    decodeList(p.SessionRequiredSingleDomain),
		RequiredPolicies:   decodeList(p.SessionRequiredPolicies),
		RequiredIdentities: decodeList(p.SessionRequiredIdentities),
		RequiredMFA:        p.SessionRequiredMFA,
	}
	if !e.Actionable() {
		return nil
	}
	return e
}

type errorBody struct {
	Code                    string                   `json:"code"`
	Message                 string                   `json:"message"`
	RequiredScopes          []string                 `json:"required_scopes"`
	AuthorizationParameters *authorizationParameters `json:"authorization_parameters"`
}

// GridFTP 530 message text carries a JSON result after this prefix, e.g.
// "530-GridFTP-JSON-Result: {... "authorization_parameters": {...}}".
const gridFtpJSONPrefix = "GridFTP-JSON-Result:"

// Fallback for an embedded result that is not valid JSON: matches the
// session_required_single_domain fragment in either its array or string form.
var embeddedDomainsR = regexp.MustCompile(`"session_required_single_domain"\s*:\s*(\[[^\]]*\]|"[^"]*")`)

// interpretGlobusError turns a non-2xx Globus API response body into an
// error. ConsentRequired and session/domain-policy errors become
// *reauth.Error so the HTTP boundary can trigger re-authentication.
//
// Other API errors use a fixed message. Only the known not-found codes are
// preserved because the folder-landing candidate loop classifies them.
func interpretGlobusError(statusCode int, body []byte) error {
	parsed := errorBody{}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Code == "" {
		return fmt.Errorf("globus request failed with status %d", statusCode)
	}
	if parsed.Code == "ConsentRequired" {
		scopes := parsed.RequiredScopes
		if len(scopes) == 0 && parsed.AuthorizationParameters != nil {
			scopes = parsed.AuthorizationParameters.RequiredScopes
		}
		if len(scopes) > 0 {
			return &reauth.Error{RequiredScopes: scopes}
		}
	}
	if e := parsed.AuthorizationParameters.sessionRequirements(); e != nil {
		return e
	}
	if e := embeddedRequirements(parsed.Message); e != nil {
		return e
	}
	return safeGlobusErrorCode(statusCode, parsed.Code)
}

func safeGlobusErrorCode(statusCode int, code string) error {
	switch code {
	case "ClientError.NotFound", "EndpointNotFound":
		// The options folder-landing loop classifies these stable API codes.
		return errors.New(code)
	default:
		return fmt.Errorf("globus request failed with status %d", statusCode)
	}
}

// decodeList accepts both JSON forms Globus uses for the session
// requirements: an array of strings or a single, possibly comma-separated,
// string.
func decodeList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	arr := []string{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return cleanList(arr)
	}
	s := ""
	if err := json.Unmarshal(raw, &s); err == nil {
		return cleanList(strings.Split(s, ","))
	}
	return nil
}

func cleanList(in []string) []string {
	out := []string{}
	for _, d := range in {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// embeddedRequirements reads the session requirements from the JSON result
// GridFTP embeds in its 530 message text.
func embeddedRequirements(message string) *reauth.Error {
	i := strings.Index(message, gridFtpJSONPrefix)
	if i < 0 {
		return nil
	}
	line := message[i+len(gridFtpJSONPrefix):]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	result := struct {
		AuthorizationParameters *authorizationParameters `json:"authorization_parameters"`
	}{}
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &result) == nil {
		return result.AuthorizationParameters.sessionRequirements()
	}
	if m := embeddedDomainsR.FindStringSubmatch(message); len(m) == 2 {
		if domains := decodeList(json.RawMessage(m[1])); len(domains) > 0 {
			return &reauth.Error{RequiredDomains: domains}
		}
	}
	return nil
}
