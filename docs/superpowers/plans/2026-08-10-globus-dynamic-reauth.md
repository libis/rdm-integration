# Globus Dynamic Re-authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect Globus `ConsentRequired` (missing `data_access` scope) and identity-domain-policy errors on every backend Globus API call, propagate them as structured HTTP 401 responses, and have both the connect and download frontends trigger a re-login with the needed scopes/domain merged into the authorize URL.

**Architecture:** A new dependency-free Go package `reauth` carries a typed error (`*reauth.Error`) from the Globus HTTP client up to a shared HTTP boundary writer that emits `401 {"reauth": {...}}`. The error also self-serializes as a `*reauth*{json}*reauth*` marker so it survives Redis string round-trips in async job paths. The Angular side gains one shared module (`shared/reauth.ts`) that extracts the structured payload (with a legacy `*scopes*` fallback), normalizes `tokenGetter` config (structured fields preferred, legacy URL-with-params parsed), and builds authorize URLs programmatically — replacing all regex/substring URL surgery.

**Tech Stack:** Go 1.x (module name `integration`, stdlib only for new code), Angular 21 + Karma/Jasmine, PrimeNG.

**Spec:** `docs/superpowers/specs/2026-08-10-globus-dynamic-reauth-design.md`

## Global Constraints

- Two repos: backend `/home/eryk/workspaces/redcap/rdm-integration` (Go), frontend `/home/eryk/workspaces/redcap/rdm-integration-frontend` (Angular). Tasks 1–4 are backend, 5–8 frontend, 9 both. Commit each task in its own repo.
- Work on branch `globus-dynamic-reauth` in **each** repo (create it in Task 1 for backend, Task 5 for frontend).
- Go tests: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/...` (targeted packages per task; full run in Task 9).
- Frontend tests: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci` (runs the whole Karma suite headless; there is no per-file runner).
- Non-reauth Globus API errors MUST keep the exact `"Code: Message"` text form — `isNotFoundError` in `options.go` string-matches `ClientError.NotFound` / `EndpointNotFound` to advance folder candidates. Never "improve" this format.
- The legacy `*scopes*<space-joined scopes>*scopes*` marker must remain parseable by the frontend (fallback for backend/frontend version skew).
- New JSON config fields use `omitempty` — configs that don't set them must serialize byte-identically to today.
- New backend files start with the header comment `// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License`; frontend files use `// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License`.
- Author of commits: default git identity, no Co-Authored-By lines.

---

### Task 1: Go `reauth` package (typed error + marker + HTTP boundary writer)

**Files:**
- Create: `image/app/core/reauth/reauth.go`
- Test: `image/app/core/reauth/reauth_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only — `encoding/json`, `errors`, `fmt`, `net/http`, `strings`).
- Produces (used by Tasks 2–3):
  - `type Error struct { RequiredScopes []string; RequiredDomains []string; Message string }` implementing `error` (pointer receiver).
  - `func Parse(s string) *Error` — extracts a marker-embedded Error from any string; nil if absent/malformed/empty.
  - `func FromError(err error) *Error` — `errors.As` for `*Error`, falling back to `Parse(err.Error())`; nil for nil/plain errors.
  - `func WriteError(w http.ResponseWriter, err error)` — reauth → `401` + `{"reauth":{...}}` JSON; otherwise `500` + `"500 - %v"` text (exactly today's format).
  - Wire format of the 401 body: `{"reauth": {"required_scopes": [...], "required_domains": [...], "message": "..."}}` (fields omitted when empty).

- [ ] **Step 1: Create backend branch**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git checkout -b globus-dynamic-reauth
```

- [ ] **Step 2: Write the failing tests**

Create `image/app/core/reauth/reauth_test.go`:

```go
package reauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorMarkerRoundTrip(t *testing.T) {
	orig := &Error{
		RequiredScopes:  []string{"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc-aef8-40ad-88df-f9c92bee82d3/data_access]"},
		RequiredDomains: []string{"sydney.edu.au"},
		Message:         "identity from sydney.edu.au required",
	}
	parsed := Parse(orig.Error())
	if parsed == nil {
		t.Fatal("expected parsed error, got nil")
	}
	if parsed.RequiredScopes[0] != orig.RequiredScopes[0] ||
		parsed.RequiredDomains[0] != "sydney.edu.au" ||
		parsed.Message != orig.Message {
		t.Fatalf("round trip mismatch: %+v", parsed)
	}
}

func TestFromErrorFindsWrappedTypedError(t *testing.T) {
	orig := &Error{RequiredDomains: []string{"sydney.edu.au"}}
	wrapped := fmt.Errorf("globus transfer failed: %w", orig)
	parsed := FromError(wrapped)
	if parsed == nil || parsed.RequiredDomains[0] != "sydney.edu.au" {
		t.Fatalf("expected domains from wrapped error, got %+v", parsed)
	}
}

func TestFromErrorFindsMarkerInsideLargerString(t *testing.T) {
	orig := &Error{RequiredScopes: []string{"scope-a"}}
	stringified := errors.New("Job failed: " + orig.Error())
	parsed := FromError(stringified)
	if parsed == nil || parsed.RequiredScopes[0] != "scope-a" {
		t.Fatalf("expected scopes from marker-embedded error, got %+v", parsed)
	}
}

func TestParseRejectsAbsentOrMalformed(t *testing.T) {
	for _, s := range []string{
		"plain error",
		"*reauth*not-json*reauth*",
		"*reauth*{}*reauth*", // no scopes and no domains -> not actionable
		"*reauth*{\"message\":\"only\"}*reauth*",
		"*reauth*unterminated",
	} {
		if got := Parse(s); got != nil {
			t.Fatalf("Parse(%q) = %+v, want nil", s, got)
		}
	}
	if got := FromError(nil); got != nil {
		t.Fatalf("FromError(nil) = %+v, want nil", got)
	}
}

func TestWriteErrorReauth(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, &Error{RequiredDomains: []string{"sydney.edu.au"}})
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(w.Result().Body)
	payload := struct {
		Reauth *Error `json:"reauth"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Reauth == nil {
		t.Fatalf("body %s did not unmarshal to reauth payload: %v", body, err)
	}
	if payload.Reauth.RequiredDomains[0] != "sydney.edu.au" {
		t.Fatalf("unexpected payload: %+v", payload.Reauth)
	}
}

func TestWriteErrorMarkerEmbedded(t *testing.T) {
	w := httptest.NewRecorder()
	inner := &Error{RequiredScopes: []string{"scope-a"}}
	WriteError(w, errors.New("Job failed: "+inner.Error()))
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWriteErrorPlain(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, errors.New("boom"))
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.HasPrefix(string(body), "500 - boom") {
		t.Fatalf("body = %q, want prefix \"500 - boom\"", body)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/core/reauth/...`
Expected: FAIL — package does not compile (`reauth.go` missing: undefined `Error`, `Parse`, `FromError`, `WriteError`).

- [ ] **Step 4: Write the implementation**

Create `image/app/core/reauth/reauth.go`:

```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/core/reauth/...`
Expected: PASS (7 tests).

- [ ] **Step 6: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git add image/app/core/reauth && git commit -m "Add reauth package: typed re-authentication error and 401 boundary writer"
```

---

### Task 2: Globus error interpretation in `DoGlobusRequest`

**Files:**
- Create: `image/app/plugin/impl/globus/errors.go`
- Create: `image/app/plugin/impl/globus/errors_test.go`
- Modify: `image/app/plugin/impl/globus/common.go` (`DoGlobusRequest`, remove `getPartialResponse` wrapper)

**Interfaces:**
- Consumes: `reauth.Error` from Task 1 (`integration/app/core/reauth`).
- Produces: `DoGlobusRequest` unchanged signature `(ctx, url, method, token string, body io.Reader) ([]byte, error)` but now returns a non-nil error for HTTP >= 400: `*reauth.Error` for consent/domain-policy errors, `fmt.Errorf("%v: %v", code, message)` for other Globus API errors (exact format — `isNotFoundError` depends on it), `fmt.Errorf("globus request failed: %d - %s", status, body)` for non-JSON bodies. Internal helper `interpretGlobusError(statusCode int, body []byte) error`.

- [ ] **Step 1: Write the failing tests**

Create `image/app/plugin/impl/globus/errors_test.go`:

```go
package globus

import (
	"context"
	"integration/app/core/reauth"
	"integration/app/plugin/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const consentRequiredBody = `{"code":"ConsentRequired","message":"Missing required data_access consent","required_scopes":["urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc-aef8-40ad-88df-f9c92bee82d3/data_access]"],"request_id":"abc123"}`

const authorizationParametersBody = `{"code":"permission_denied","message":"None of your authenticated identities are from domains allowed by resource policies","authorization_parameters":{"session_message":"Sydney requires institutional login","session_required_single_domain":["sydney.edu.au"]},"http_response_code":403}`

const authorizationParametersStringBody = `{"code":"permission_denied","message":"policy","authorization_parameters":{"session_required_single_domain":"sydney.edu.au,usyd.edu.au"}}`

const gridFtpEmbeddedBody = `{"code":"ExternalError.DirListingFailed.LoginFailed","message":"Login Failed\n530-Login incorrect. : GlobusError: v=1 c=LOGIN_DENIED\n530-GridFTP-Message: None of your authenticated identities are from domains allowed by resource policies\n530-GridFTP-JSON-Result: {\"DATA_TYPE\": \"result#1.1.0\", \"authorization_parameters\": {\"session_required_single_domain\": [\"sydney.edu.au\"]}, \"code\": \"permission_denied\", \"http_response_code\": 403}\n530 End.","request_id":"xyz","resource":"/operation/endpoint/x/ls"}`

const notFoundBody = `{"code":"ClientError.NotFound","message":"Directory not found"}`

func TestInterpretGlobusError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantScopes  []string
		wantDomains []string
		wantPlain   string // expected error text when no reauth expected
	}{
		{
			name:       "consent_required_becomes_scopes",
			status:     403,
			body:       consentRequiredBody,
			wantScopes: []string{"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc-aef8-40ad-88df-f9c92bee82d3/data_access]"},
		},
		{
			name:        "authorization_parameters_array_becomes_domains",
			status:      403,
			body:        authorizationParametersBody,
			wantDomains: []string{"sydney.edu.au"},
		},
		{
			name:        "authorization_parameters_comma_string_becomes_domains",
			status:      403,
			body:        authorizationParametersStringBody,
			wantDomains: []string{"sydney.edu.au", "usyd.edu.au"},
		},
		{
			name:        "gridftp_embedded_json_becomes_domains",
			status:      502,
			body:        gridFtpEmbeddedBody,
			wantDomains: []string{"sydney.edu.au"},
		},
		{
			name:      "not_found_keeps_code_message_format",
			status:    404,
			body:      notFoundBody,
			wantPlain: "ClientError.NotFound: Directory not found",
		},
		{
			name:      "non_json_body_keeps_status_and_body",
			status:    502,
			body:      "<html>bad gateway</html>",
			wantPlain: "globus request failed: 502 - <html>bad gateway</html>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := interpretGlobusError(tt.status, []byte(tt.body))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			re := reauth.FromError(err)
			if len(tt.wantScopes) == 0 && len(tt.wantDomains) == 0 {
				if re != nil {
					t.Fatalf("expected plain error, got reauth %+v", re)
				}
				if err.Error() != tt.wantPlain {
					t.Fatalf("error text = %q, want %q", err.Error(), tt.wantPlain)
				}
				return
			}
			if re == nil {
				t.Fatalf("expected reauth error, got %v", err)
			}
			if len(tt.wantScopes) > 0 && (len(re.RequiredScopes) != len(tt.wantScopes) || re.RequiredScopes[0] != tt.wantScopes[0]) {
				t.Fatalf("scopes = %v, want %v", re.RequiredScopes, tt.wantScopes)
			}
			if len(tt.wantDomains) > 0 {
				if len(re.RequiredDomains) != len(tt.wantDomains) {
					t.Fatalf("domains = %v, want %v", re.RequiredDomains, tt.wantDomains)
				}
				for i := range tt.wantDomains {
					if re.RequiredDomains[i] != tt.wantDomains[i] {
						t.Fatalf("domains = %v, want %v", re.RequiredDomains, tt.wantDomains)
					}
				}
			}
		})
	}
}

func TestDoGlobusRequestReturnsInterpretedErrorOn403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(consentRequiredBody))
	}))
	defer server.Close()
	_, err := DoGlobusRequest(context.Background(), server.URL, "GET", "token", nil)
	if reauth.FromError(err) == nil {
		t.Fatalf("expected reauth error from 403 response, got %v", err)
	}
}

func TestDoGlobusRequestStillReturnsBodyOn200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"DATA":[]}`))
	}))
	defer server.Close()
	b, err := DoGlobusRequest(context.Background(), server.URL, "GET", "token", nil)
	if err != nil || string(b) != `{"DATA":[]}` {
		t.Fatalf("got body %q err %v", b, err)
	}
}

// Regression guard for the folder-landing candidate loop: a 404 must still
// classify as not-found (so candidates advance), and a reauth error hitting
// every candidate must propagate out of Options as the returned error.
func TestOptionsCandidateLoopWithInterpretedErrors(t *testing.T) {
	t.Run("not_found_classification_survives", func(t *testing.T) {
		err := interpretGlobusError(404, []byte(notFoundBody))
		if !isNotFoundError(err) {
			t.Fatalf("isNotFoundError(%v) = false, want true", err)
		}
	})
	t.Run("reauth_propagates_when_all_candidates_fail", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/ls") || strings.Contains(r.URL.Path, "/endpoint/") {
				w.WriteHeader(403)
				w.Write([]byte(consentRequiredBody))
				return
			}
			w.Write([]byte(`{}`))
		}))
		defer server.Close()
		_, err := Options(context.Background(), types.OptionsRequest{
			Url:      server.URL,
			Token:    "token",
			RepoName: "82c495cc-aef8-40ad-88df-f9c92bee82d3",
		})
		if reauth.FromError(err) == nil {
			t.Fatalf("expected reauth error to propagate from Options, got %v", err)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/plugin/impl/globus/...`
Expected: FAIL — `interpretGlobusError` undefined; `TestDoGlobusRequestReturnsInterpretedErrorOn403` fails (no error returned for 403 today).

- [ ] **Step 3: Create `errors.go`**

Create `image/app/plugin/impl/globus/errors.go`:

```go
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
```

- [ ] **Step 4: Wire status handling into `DoGlobusRequest` and remove the old marker wrapper**

In `image/app/plugin/impl/globus/common.go`:

Replace the body of `DoGlobusRequest`:

```go
func DoGlobusRequest(ctx context.Context, url, method, token string, body io.Reader) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if r.StatusCode >= 400 {
		return b, interpretGlobusError(r.StatusCode, b)
	}
	return b, nil
}
```

Delete the `getPartialResponse` wrapper function (lines with the `*scopes*` marker):

```go
// DELETE this function entirely:
func getPartialResponse(ctx context.Context, url string, token string, limit, offset int) (Response, error) {
	res, err := doGetPartialResponse(ctx, url, token, limit, offset)
	if err != nil && strings.HasPrefix(err.Error(), "ConsentRequired") {
		return res, fmt.Errorf("*scopes*%v*scopes*", strings.Join(res.RequiredScopes, " "))
	}
	return res, err
}
```

In `getResponseWithPath`, change the call `getPartialResponse(ctx, url, token, limit, offset)` to `doGetPartialResponse(ctx, url, token, limit, offset)`.

The `RequiredScopes` field on the `Response` struct becomes unused by production code — leave it in place (it documents the API shape and is harmless), but if the compiler or linter flags unused imports (`strings` is still used elsewhere in the file), fix imports only.

- [ ] **Step 5: Run the full globus package tests**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/plugin/impl/globus/...`
Expected: PASS, including all pre-existing tests in `common_test.go`, `options_test.go`, `streams_test.go` (they stub responses with HTTP 200, so the new status check does not affect them). If any pre-existing test stubbed an error via a non-200 status, update that test's expectation to the new interpreted error text — do not change production formats.

- [ ] **Step 6: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git add image/app/plugin/impl/globus && git commit -m "Interpret Globus consent and domain-policy errors as typed reauth errors"
```

---

### Task 3: Apply the 401 boundary writer in HTTP handlers

**Files:**
- Modify: `image/app/plugin/funcs/options/options.go:39-44`
- Modify: `image/app/plugin/funcs/search/search.go:38-43`
- Modify: `image/app/common/compare.go:75-79, 111-116`
- Modify: `image/app/common/download.go:60-65, 108-113`
- Modify: `image/app/common/store.go:76-84`

**Interfaces:**
- Consumes: `reauth.WriteError(w, err)` from Task 1.
- Produces: every listed endpoint returns `401 {"reauth": {...}}` when the underlying error is/embeds a `reauth.Error`; unchanged `500 - <text>` otherwise. Task 5+ frontend code relies on: status 401, JSON body key `reauth` with `required_scopes` / `required_domains` / `message`.

- [ ] **Step 1: Apply the substitutions**

Each change replaces a `w.WriteHeader(http.StatusInternalServerError); w.Write(...)` pair with `reauth.WriteError(w, err)`. Add `"integration/app/core/reauth"` to each file's imports (and `"errors"` where noted). Only the listed branches change — "bad request" / marshal-failure branches stay as they are.

Note: `plugin/funcs/compare/compare.go` needs **no** change even though the spec's handler list names it — its plugin work runs in a goroutine and stringifies errors into `CachedResponse.ErrorMessage`, so Globus errors from that flow surface exclusively through `GetCachedResponse` (covered above); the handler's own error branches are request-parse/marshal failures only.

`image/app/plugin/funcs/options/options.go` — the plugin call error branch:

```go
	res, err := plugin.GetPlugin(params.Plugin).Options(r.Context(), params)
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
```

`image/app/plugin/funcs/search/search.go` — the plugin call error branch:

```go
	res, err := plugin.GetPlugin(params.Plugin).Search(r.Context(), params)
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
```

`image/app/common/compare.go` — in `GetCachedResponse`, the cached-error branch (the async compare job stringifies plugin errors into `ErrorMessage`, so the marker arrives inside this string; add `"errors"` import):

```go
	if res.ErrorMessage != "" {
		reauth.WriteError(w, errors.New(res.ErrorMessage))
		return
	}
```

`image/app/common/compare.go` — in `Compare`, the job-error branch:

```go
	errMessage := config.GetRedis().Get(r.Context(), fmt.Sprintf("error %v", req.PersistentId))
	if errMessage != nil && errMessage.Val() != "" {
		reauth.WriteError(w, fmt.Errorf("Job failed: %v", errMessage))
		return
	}
```

`image/app/common/download.go` — in `Download`, the `globus.Download` error branch:

```go
	res, err = globus.Download(r.Context(), req.StreamParams, selected)
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
```

`image/app/common/download.go` — in `GlobusTransferStatus`, the `GetTaskStatus` error branch (the existing `401 - globus session expired` branch above it stays untouched):

```go
	resBytes, err := globus.GetTaskStatus(r.Context(), token, taskId)
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
```

`image/app/common/store.go` — in `Store`, the `DoWork`/`AddJob` error branch:

```go
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
```

- [ ] **Step 2: Build and run affected package tests**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go build ./app/... && go test ./app/common/... ./app/plugin/...`
Expected: build succeeds; existing tests PASS (handler behavior for plain errors is byte-identical: `500 - <text>` — except the `Compare` job-error branch, which previously had no `500 - ` prefix and now gains one; accepted by ruling during execution, no consumer matches the exact text). The reauth branch itself is covered by Task 1's `WriteError` tests plus Task 2's propagation test; handler-level Redis-dependent integration tests are intentionally not added (no Redis test double exists in this repo — verified in `common/ddi_cdi_test.go`).

- [ ] **Step 3: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git add image/app/plugin/funcs image/app/common && git commit -m "Return structured 401 reauth responses from Globus-facing handlers"
```

---

### Task 4: Structured `TokenGetter` config fields (Go)

**Files:**
- Modify: `image/app/config/frontend_config.go:31-34`
- Create: `image/app/config/frontend_config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.TokenGetter` gains `Scopes []string` (`json:"scopes,omitempty"`) and `SessionRequiredSingleDomain []string` (`json:"session_required_single_domain,omitempty"`). These flow verbatim through `/api/frontend/config` (the handler marshals the same struct). Task 5's frontend `TokenGetter` interface mirrors these JSON names.

- [ ] **Step 1: Write the failing test**

Create `image/app/config/frontend_config_test.go`:

```go
package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTokenGetterStructuredFieldsRoundTrip(t *testing.T) {
	in := `{"URL":"https://auth.globus.org/v2/oauth2/authorize","oauth_client_id":"abc","scopes":["urn:globus:auth:scope:transfer.api.globus.org:all","openid","email","profile"],"session_required_single_domain":["kuleuven.be"]}`
	tg := TokenGetter{}
	if err := json.Unmarshal([]byte(in), &tg); err != nil {
		t.Fatal(err)
	}
	if len(tg.Scopes) != 4 || tg.Scopes[1] != "openid" {
		t.Fatalf("scopes not parsed: %+v", tg)
	}
	if len(tg.SessionRequiredSingleDomain) != 1 || tg.SessionRequiredSingleDomain[0] != "kuleuven.be" {
		t.Fatalf("domains not parsed: %+v", tg)
	}
	out, err := json.Marshal(tg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"scopes"`, `"session_required_single_domain"`, `"kuleuven.be"`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("marshalled config %s missing %s", out, want)
		}
	}
}

func TestTokenGetterLegacyConfigSerializesUnchanged(t *testing.T) {
	legacy := TokenGetter{
		Url:           "https://auth.globus.org/v2/oauth2/authorize?scope=x&session_required_single_domain=kuleuven.be",
		OauthClientId: "abc",
	}
	out, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`"scopes"`, `"session_required_single_domain":[`} {
		if strings.Contains(string(out), banned) {
			t.Fatalf("omitempty violated, legacy config gained %s: %s", banned, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/config/...`
Expected: FAIL — `tg.Scopes` undefined.

- [ ] **Step 3: Add the fields**

In `image/app/config/frontend_config.go` replace the `TokenGetter` struct:

```go
type TokenGetter struct {
	Url                         string   `json:"URL,omitempty"`
	OauthClientId               string   `json:"oauth_client_id,omitempty"`
	Scopes                      []string `json:"scopes,omitempty"`
	SessionRequiredSingleDomain []string `json:"session_required_single_domain,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go test ./app/config/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git add image/app/config && git commit -m "Add structured scopes and session_required_single_domain to TokenGetter config"
```

---

### Task 5: Frontend shared reauth module

**Files:**
- Modify: `src/app/models/plugin.ts` (export `TokenGetter`, add fields)
- Create: `src/app/shared/reauth.ts`
- Test: `src/app/shared/reauth.spec.ts`

(All frontend paths are relative to `/home/eryk/workspaces/redcap/rdm-integration-frontend`.)

**Interfaces:**
- Consumes: `TokenGetter` from `models/plugin.ts`.
- Produces (used by Tasks 6–8):
  - `interface ReauthRequest { scopes?: string[]; domains?: string[]; message?: string }`
  - `function extractReauth(err: unknown): ReauthRequest | undefined` — structured 401 payload first, legacy `*scopes*` string fallback second.
  - `interface AuthorizeBase { authorizeUrl: string; clientId: string; baseScopes: string[]; baseDomains: string[] }`
  - `function normalizeTokenGetter(tg: TokenGetter | undefined, resolvedUrl: string): AuthorizeBase | undefined` — undefined when no client id or `resolvedUrl` is not absolute.
  - `function buildAuthorizeUrl(base: AuthorizeBase, opts: { redirectUri: string; state: string; reauth?: ReauthRequest; guestMode?: boolean }): string`
  - `function storePendingReauth(reauth: ReauthRequest): void` / `function takePendingReauth(): ReauthRequest | undefined` — sessionStorage handoff (key `pendingReauth`), read-and-clear.

- [ ] **Step 1: Create frontend branch**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration-frontend && git checkout -b globus-dynamic-reauth
```

- [ ] **Step 2: Export and extend `TokenGetter` in `src/app/models/plugin.ts`**

Replace the private `interface TokenGetter` block with:

```ts
export interface TokenGetter {
  URL?: string;
  oauth_client_id?: string;
  scopes?: string[];
  session_required_single_domain?: string[];
}
```

- [ ] **Step 3: Write the failing spec**

Create `src/app/shared/reauth.spec.ts`:

```ts
// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

import {
  buildAuthorizeUrl,
  extractReauth,
  normalizeTokenGetter,
  storePendingReauth,
  takePendingReauth,
} from './reauth';

describe('extractReauth', () => {
  it('extracts structured 401 payload', () => {
    const err = {
      status: 401,
      error: {
        reauth: {
          required_scopes: ['scope-a'],
          required_domains: ['sydney.edu.au'],
          message: 'institutional login required',
        },
      },
    };
    expect(extractReauth(err)).toEqual({
      scopes: ['scope-a'],
      domains: ['sydney.edu.au'],
      message: 'institutional login required',
    });
  });

  it('extracts legacy *scopes* marker from string error body', () => {
    const err = { status: 500, error: '500 - *scopes*scope-a scope-b*scopes*' };
    expect(extractReauth(err)).toEqual({ scopes: ['scope-a', 'scope-b'] });
  });

  it('extracts legacy marker from message field', () => {
    expect(extractReauth({ message: '*scopes*repo.read*scopes*' })).toEqual({
      scopes: ['repo.read'],
    });
  });

  it('returns undefined for plain errors and empty payloads', () => {
    expect(extractReauth({ status: 500, error: 'boom' })).toBeUndefined();
    expect(extractReauth({ status: 401, error: 'session expired' })).toBeUndefined();
    expect(extractReauth({ status: 401, error: { reauth: {} } })).toBeUndefined();
    expect(extractReauth(undefined)).toBeUndefined();
  });
});

describe('normalizeTokenGetter', () => {
  const legacyUrl =
    'https://auth.globus.org/v2/oauth2/authorize?scope=urn%3Aglobus%3Aauth%3Ascope%3Atransfer.api.globus.org%3Aall+openid+email+profile&session_required_single_domain=kuleuven.be';

  it('prefers structured fields when present', () => {
    const base = normalizeTokenGetter(
      {
        URL: 'https://auth.globus.org/v2/oauth2/authorize',
        oauth_client_id: 'client-1',
        scopes: ['scope-a', 'openid'],
        session_required_single_domain: ['kuleuven.be'],
      },
      'https://auth.globus.org/v2/oauth2/authorize',
    )!;
    expect(base.authorizeUrl).toBe('https://auth.globus.org/v2/oauth2/authorize');
    expect(base.clientId).toBe('client-1');
    expect(base.baseScopes).toEqual(['scope-a', 'openid']);
    expect(base.baseDomains).toEqual(['kuleuven.be']);
  });

  it('parses legacy URL query params and strips them from the base URL', () => {
    const base = normalizeTokenGetter(
      { URL: legacyUrl, oauth_client_id: 'client-1' },
      legacyUrl,
    )!;
    expect(base.baseScopes).toEqual([
      'urn:globus:auth:scope:transfer.api.globus.org:all',
      'openid',
      'email',
      'profile',
    ]);
    expect(base.baseDomains).toEqual(['kuleuven.be']);
    expect(base.authorizeUrl).toBe('https://auth.globus.org/v2/oauth2/authorize');
  });

  it('preserves unrelated query params in the base URL', () => {
    const base = normalizeTokenGetter(
      {
        URL: 'https://example.org/authorize?scope=a&foo=bar',
        oauth_client_id: 'client-1',
      },
      'https://example.org/authorize?scope=a&foo=bar',
    )!;
    expect(base.authorizeUrl).toBe('https://example.org/authorize?foo=bar');
    expect(base.baseScopes).toEqual(['a']);
  });

  it('returns undefined without client id or absolute URL', () => {
    expect(normalizeTokenGetter({ URL: legacyUrl }, legacyUrl)).toBeUndefined();
    expect(
      normalizeTokenGetter(
        { URL: 'settings/tokens', oauth_client_id: 'client-1' },
        'https://github.com/settings/tokens'.replace('https://', ''),
      ),
    ).toBeUndefined();
  });
});

describe('buildAuthorizeUrl', () => {
  const base = {
    authorizeUrl: 'https://auth.globus.org/v2/oauth2/authorize',
    clientId: 'client-1',
    baseScopes: [
      'urn:globus:auth:scope:transfer.api.globus.org:all',
      'openid',
      'email',
      'profile',
    ],
    baseDomains: ['kuleuven.be'],
  };
  const opts = { redirectUri: 'https://app.example.org/connect', state: '{"nonce":"n"}' };

  function params(url: string): URLSearchParams {
    return new URL(url).searchParams;
  }

  it('builds default URL with base scopes and domains', () => {
    const p = params(buildAuthorizeUrl(base, opts));
    expect(p.get('scope')).toBe(
      'urn:globus:auth:scope:transfer.api.globus.org:all openid email profile',
    );
    expect(p.get('session_required_single_domain')).toBe('kuleuven.be');
    expect(p.get('client_id')).toBe('client-1');
    expect(p.get('redirect_uri')).toBe('https://app.example.org/connect');
    expect(p.get('response_type')).toBe('code');
    expect(p.get('state')).toBe('{"nonce":"n"}');
    expect(p.get('prompt')).toBeNull();
  });

  it('merges required scopes by base prefix, keeping other base scopes', () => {
    const consented =
      'urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/x/data_access]';
    const p = params(
      buildAuthorizeUrl(base, { ...opts, reauth: { scopes: [consented] } }),
    );
    expect(p.get('scope')).toBe(`${consented} openid email profile`);
  });

  it('appends unmatched required scopes', () => {
    const p = params(
      buildAuthorizeUrl(
        { ...base, baseScopes: [] },
        { ...opts, reauth: { scopes: ['repo.read'] } },
      ),
    );
    expect(p.get('scope')).toBe('repo.read');
  });

  it('replaces domains and forces fresh login when reauth demands domains', () => {
    const p = params(
      buildAuthorizeUrl(base, { ...opts, reauth: { domains: ['sydney.edu.au'] } }),
    );
    expect(p.get('session_required_single_domain')).toBe('sydney.edu.au');
    expect(p.get('prompt')).toBe('login');
  });

  it('omits base domains in guest mode', () => {
    const p = params(buildAuthorizeUrl(base, { ...opts, guestMode: true }));
    expect(p.get('session_required_single_domain')).toBeNull();
    expect(p.get('prompt')).toBeNull();
  });

  it('applies error-demanded domains even in guest mode', () => {
    const p = params(
      buildAuthorizeUrl(base, {
        ...opts,
        guestMode: true,
        reauth: { domains: ['sydney.edu.au'] },
      }),
    );
    expect(p.get('session_required_single_domain')).toBe('sydney.edu.au');
    expect(p.get('prompt')).toBe('login');
  });
});

describe('pending reauth storage', () => {
  afterEach(() => sessionStorage.removeItem('pendingReauth'));

  it('stores and takes exactly once', () => {
    storePendingReauth({ domains: ['sydney.edu.au'] });
    expect(takePendingReauth()).toEqual({ domains: ['sydney.edu.au'] });
    expect(takePendingReauth()).toBeUndefined();
  });

  it('survives malformed storage content', () => {
    sessionStorage.setItem('pendingReauth', 'not-json');
    expect(takePendingReauth()).toBeUndefined();
  });
});
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: FAIL — module `./reauth` not found.

- [ ] **Step 5: Write the implementation**

Create `src/app/shared/reauth.ts`:

```ts
// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

import { TokenGetter } from '../models/plugin';

/** What a new OAuth login must include, as demanded by a backend error. */
export interface ReauthRequest {
  scopes?: string[];
  domains?: string[];
  message?: string;
}

/** Normalized token-getter configuration for OAuth authorize URL building. */
export interface AuthorizeBase {
  authorizeUrl: string;
  clientId: string;
  baseScopes: string[];
  baseDomains: string[];
}

interface ReauthPayload {
  required_scopes?: string[];
  required_domains?: string[];
  message?: string;
}

const LEGACY_MARKER = '*scopes*';
const PENDING_REAUTH_KEY = 'pendingReauth';

/**
 * Inspect a failed HTTP response for a re-authentication demand.
 * Primary: structured `401 {"reauth": {...}}` responses from the backend.
 * Fallback: the legacy `*scopes*<space-joined scopes>*scopes*` marker in
 * plain-text bodies (kept for backend/frontend version skew).
 */
export function extractReauth(err: unknown): ReauthRequest | undefined {
  const e = err as
    | { status?: number; error?: unknown; message?: unknown }
    | undefined;
  if (!e) return undefined;
  if (e.status === 401 && typeof e.error === 'object' && e.error !== null) {
    const payload = (e.error as { reauth?: ReauthPayload }).reauth;
    if (payload) {
      const res: ReauthRequest = {};
      if (payload.required_scopes?.length) res.scopes = payload.required_scopes;
      if (payload.required_domains?.length)
        res.domains = payload.required_domains;
      if (payload.message) res.message = payload.message;
      if (res.scopes || res.domains) return res;
    }
    return undefined;
  }
  const text =
    (typeof e.error === 'string' ? e.error : undefined) ??
    (typeof e.message === 'string' ? e.message : undefined) ??
    '';
  const first = text.indexOf(LEGACY_MARKER);
  const last = text.lastIndexOf(LEGACY_MARKER);
  if (first < 0 || last <= first) return undefined;
  const scopes = text
    .substring(first + LEGACY_MARKER.length, last)
    .split(' ')
    .filter((s) => s.length > 0);
  return scopes.length > 0 ? { scopes } : undefined;
}

/**
 * Normalize a token getter into an AuthorizeBase. Structured config fields
 * (scopes, session_required_single_domain) are preferred; when absent they
 * are parsed out of the legacy URL query string and stripped from the base
 * URL. Returns undefined when there is no OAuth client id or the resolved
 * URL is not absolute (non-OAuth token getters open a plain window instead).
 */
export function normalizeTokenGetter(
  tg: TokenGetter | undefined,
  resolvedUrl: string,
): AuthorizeBase | undefined {
  if (!tg?.oauth_client_id || !resolvedUrl.includes('://')) return undefined;
  let url: URL;
  try {
    url = new URL(resolvedUrl);
  } catch {
    return undefined;
  }
  const urlScope = url.searchParams.get('scope');
  const urlDomains = url.searchParams.get('session_required_single_domain');
  url.searchParams.delete('scope');
  url.searchParams.delete('session_required_single_domain');
  const baseScopes =
    tg.scopes ?? (urlScope ? urlScope.split(/[\s+]+/).filter(Boolean) : []);
  const baseDomains =
    tg.session_required_single_domain ??
    (urlDomains ? urlDomains.split(',').filter(Boolean) : []);
  return {
    authorizeUrl: url.toString(),
    clientId: tg.oauth_client_id,
    baseScopes,
    baseDomains,
  };
}

/**
 * Merge required scopes into the base scope list: a required scope replaces
 * the base entry it extends (same prefix before `[`), unmatched required
 * scopes are appended, all other base entries are preserved. Keeping
 * openid/email/profile intact is essential — dropping them breaks the
 * backend userinfo (getPrincipal) call after re-login.
 */
function mergeScopes(baseScopes: string[], required: string[]): string[] {
  const result = [...baseScopes];
  for (const req of required) {
    const reqBase = req.split('[')[0];
    const i = result.findIndex((s) => s.split('[')[0] === reqBase);
    if (i >= 0) result[i] = req;
    else result.push(req);
  }
  return result;
}

/**
 * Build the OAuth authorize URL. Reauth-demanded domains replace the
 * configured ones and force a fresh login (`prompt=login`); guest mode omits
 * the configured domains (any Globus identity may be used) unless an error
 * explicitly demanded one.
 */
export function buildAuthorizeUrl(
  base: AuthorizeBase,
  opts: {
    redirectUri: string;
    state: string;
    reauth?: ReauthRequest;
    guestMode?: boolean;
  },
): string {
  const url = new URL(base.authorizeUrl);
  const scopes = mergeScopes(base.baseScopes, opts.reauth?.scopes ?? []);
  if (scopes.length > 0) {
    url.searchParams.set('scope', scopes.join(' '));
  }
  const demanded = opts.reauth?.domains ?? [];
  const domains =
    demanded.length > 0 ? demanded : opts.guestMode ? [] : base.baseDomains;
  if (domains.length > 0) {
    url.searchParams.set('session_required_single_domain', domains.join(','));
  }
  if (demanded.length > 0) {
    url.searchParams.set('prompt', 'login');
  }
  url.searchParams.set('client_id', base.clientId);
  url.searchParams.set('redirect_uri', opts.redirectUri);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('state', opts.state);
  return url.toString();
}

/** Persist a reauth demand across an in-app navigation (compare -> connect). */
export function storePendingReauth(reauth: ReauthRequest): void {
  try {
    sessionStorage.setItem(PENDING_REAUTH_KEY, JSON.stringify(reauth));
  } catch {
    // Storage unavailable (private mode) — the user can re-authorize manually.
  }
}

/** Read and clear a stored reauth demand. */
export function takePendingReauth(): ReauthRequest | undefined {
  try {
    const raw = sessionStorage.getItem(PENDING_REAUTH_KEY);
    sessionStorage.removeItem(PENDING_REAUTH_KEY);
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as ReauthRequest;
    return parsed.scopes || parsed.domains ? parsed : undefined;
  } catch {
    return undefined;
  }
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: PASS — all new `reauth.spec.ts` tests green; no pre-existing spec touched yet.

- [ ] **Step 7: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration-frontend && git add src/app/models/plugin.ts src/app/shared/reauth.ts src/app/shared/reauth.spec.ts && git commit -m "Add shared reauth module: extraction, token-getter normalization, authorize URL builder"
```

---

### Task 6: Wire the connect component

**Files:**
- Modify: `src/app/connect/connect.component.ts` (`getRepoToken` ~line 705, `handleScopesError` ~line 1262, `getOptions` error handler ~line 1233, `ngOnInit` ~line 364)
- Modify: `src/app/connect/connect.component.advanced.spec.ts` (~lines 323–366, 688–706)
- Modify: `src/app/connect/connect.component.behavior.spec.ts` (~lines 649–667)

**Interfaces:**
- Consumes: `extractReauth`, `normalizeTokenGetter`, `buildAuthorizeUrl`, `takePendingReauth`, `ReauthRequest` from `src/app/shared/reauth`.
- Produces: `getRepoToken(reauth?: ReauthRequest)` (public — template "Authorize" button keeps calling it with no args); `private handleReauthError(err: unknown): boolean` replacing `handleScopesError`.

- [ ] **Step 1: Update the failing/changed specs first**

In `connect.component.advanced.spec.ts` (~line 688), the test `getOptions requests additional scopes via getRepoToken when signalled` errors with `{ error: '*scopes*repo:read*scopes*' }` and expects `getRepoToken` to be called with `'repo:read'`. Change the expectation to the new object form (legacy marker still parsed by `extractReauth`):

```ts
expect(comp.getRepoToken).toHaveBeenCalledWith({ scopes: ['repo:read'] });
```

Add a sibling test right after it for the structured payload:

```ts
it('getOptions triggers reauth on structured 401 payload with domains', async () => {
  // Same arrangement as the previous test, but the error is:
  // observer.error({
  //   status: 401,
  //   error: { reauth: { required_domains: ['sydney.edu.au'] } },
  // })
  // Expectation:
  expect(comp.getRepoToken).toHaveBeenCalledWith({
    domains: ['sydney.edu.au'],
  });
});
```

(Copy the arrangement code — component setup, `repoLookupService.getOptions` spy — verbatim from the `*scopes*` test above it; only the error object and expectation differ.)

In `connect.component.behavior.spec.ts` (~line 667) change:

```ts
expect(comp.getRepoToken).toHaveBeenCalledOnceWith({ scopes: ['repo.read'] });
```

In `connect.component.advanced.spec.ts` (~line 332) `getRepoToken builds redirect url including dataset`: the URL is now built with `URL`/`URLSearchParams`, so encoding may differ (e.g. `+` vs `%20`, param order `scope`, `session_required_single_domain`, `client_id`, `redirect_uri`, `response_type`, `state`). Update assertions to decode instead of string-matching raw fragments where they break, e.g.:

```ts
const assigned = navigationSpy.assign.calls.mostRecent().args[0] as string;
const p = new URL(assigned).searchParams;
expect(p.get('client_id')).toBe('id');
expect(JSON.parse(p.get('state')!).datasetId?.value).toBe('doi:10/ABC');
```

(Adapt names to the actual spy/setup in that file — read the existing test body before editing; only assertions change, not the arrangement.)

- [ ] **Step 2: Run tests to verify the updated specs fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: FAIL — `getRepoToken` still receives a string; structured-payload test fails; URL-building test may fail.

- [ ] **Step 3: Rewrite `getRepoToken` and the error handlers**

In `src/app/connect/connect.component.ts`, add imports:

```ts
import {
  ReauthRequest,
  buildAuthorizeUrl,
  extractReauth,
  normalizeTokenGetter,
  takePendingReauth,
} from '../shared/reauth';
```

Replace `getRepoToken(scopes?: string)` with:

```ts
getRepoToken(reauth?: ReauthRequest) {
  const pId = this.pluginId();
  if (pId === undefined) {
    this.notificationService.showError('Repository type is missing');
    return;
  }
  const tg = this.pluginService.getPlugin(pId).tokenGetter!;
  let url = this.url() + (tg.URL === undefined ? '' : tg.URL);
  if (tg.URL?.includes('://')) {
    url = tg.URL;
  }
  const base = normalizeTokenGetter(tg, url);
  if (base) {
    const nonce = this.newNonce(44);
    const pluginIdItem = this.getItem(this.pluginIds(), pId);
    if (pluginIdItem !== undefined) {
      pluginIdItem.hidden = this.pluginIdSelectHidden();
    }
    const loginState: LoginState = {
      sourceUrl: this.sourceUrl(),
      url: this.url(),
      plugin: this.getItem(this.plugins(), this.plugin()),
      pluginId: pluginIdItem,
      repoName: this.computedRepoName(),
      user: this.user(),
      option: this.getItem(this.branchItems(), this.option()),
      datasetId: this.getItem(this.doiItems(), this.datasetId()),
      collectionId: this.getItem(this.collectionItems(), this.collectionId()),
      nonce: nonce,
    };
    this.navigation.assign(
      buildAuthorizeUrl(base, {
        redirectUri: this.pluginService.getRedirectUri(),
        state: JSON.stringify(loginState),
        reauth,
      }),
    );
  } else {
    const curUrl = this.url();
    if (curUrl) window.open(curUrl, '_blank');
  }
}
```

Replace `handleScopesError` (keep the doc comment style, update content) with:

```ts
/**
 * Inspect a failed HTTP response for a re-authentication demand — either the
 * structured 401 {"reauth": ...} payload or the legacy *scopes* marker — and
 * trigger a new authorization with the demanded scopes/domains merged into
 * the authorize URL. Returns true when re-auth was triggered so callers can
 * skip regular error handling.
 */
private handleReauthError(err: unknown): boolean {
  const reauth = extractReauth(err);
  if (!reauth) return false;
  this.getRepoToken(reauth);
  return true;
}
```

Update the two call sites at ~lines 383 and 392 from `this.handleScopesError(err)` to `this.handleReauthError(err)`.

In `getOptions` (~line 1233), replace the inline marker parsing:

```ts
error: (err) => {
  if (this.handleReauthError(err)) return;
  this.notificationService.showError(`Branch lookup failed: ${err.error}`);
  this.branchItems.set([]);
  this.option.set(undefined);
  this.optionsLoading.set(false);
},
```

In `ngOnInit`, immediately after `this.attemptFullRestore('[ngOnInit]');`, add the pending-reauth pickup (Task 8 stores it when the compare flow hits a reauth error):

```ts
const pendingReauth = takePendingReauth();
if (pendingReauth && this.pluginId()) {
  // A reauth demand stored before navigating here (e.g. from the compare
  // polling flow) — go straight back to the OAuth provider.
  this.getRepoToken(pendingReauth);
  return;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: PASS — updated connect specs green, no other spec regressions. If other connect specs assert on the old authorize URL format, update their assertions the same way as Step 1's URL test (decode with `new URL(...)`, assert params).

- [ ] **Step 5: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration-frontend && git add src/app/connect && git commit -m "Connect: structured reauth handling and programmatic authorize URL building"
```

---

### Task 7: Wire the download component (the reported bug)

**Files:**
- Modify: `src/app/download/download.component.ts` (`getRepoToken` ~line 1258, `getOptions` error handler ~line 1147, `download()` error handler ~line 1018, repo search handlers ~lines 462–479)
- Modify: `src/app/download/download.component.spec.ts` (~lines 654, 724, 738, 751 and new regression specs)

**Interfaces:**
- Consumes: `extractReauth`, `normalizeTokenGetter`, `buildAuthorizeUrl`, `ReauthRequest` from `src/app/shared/reauth`.
- Produces: `getRepoToken(reauth?: ReauthRequest)` (template keeps calling with no args); `private handleReauthError(err: unknown): boolean`.

- [ ] **Step 1: Write the failing regression specs**

Add to `src/app/download/download.component.spec.ts` (alongside the existing `getOptions`/`getRepoToken` tests; copy the component setup pattern used by the neighboring tests in that file):

```ts
it('getOptions triggers reauth instead of raw error on structured 401 (scope consent)', () => {
  // Arrange like the existing getOptions error test in this file, with the
  // repo lookup service spy erroring with:
  const reauthErr = {
    status: 401,
    error: {
      reauth: {
        required_scopes: [
          'urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc/data_access]',
        ],
      },
    },
  };
  spyOn(component, 'getRepoToken');
  // ...trigger component.getOptions() with the spy erroring reauthErr...
  expect(component.getRepoToken).toHaveBeenCalledWith({
    scopes: [
      'urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc/data_access]',
    ],
  });
  // And the raw "Branch lookup failed" notification must NOT have fired:
  expect(notificationService.showError).not.toHaveBeenCalled();
});

it('download() triggers reauth on structured 401 with domains', () => {
  // Arrange like the existing download() error test, with submit.download
  // erroring:
  const reauthErr = {
    status: 401,
    error: { reauth: { required_domains: ['sydney.edu.au'] } },
  };
  spyOn(component, 'getRepoToken');
  // ...trigger component.download() with the spy erroring reauthErr...
  expect(component.getRepoToken).toHaveBeenCalledWith({
    domains: ['sydney.edu.au'],
  });
});
```

(The existing specs in this file show the exact TestBed setup, how `repoLookupService`/`submit` are spied, and how `notificationService` is injected — mirror them; only the error payloads and expectations above are new.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: FAIL — download component has no reauth handling; `getRepoToken` not called; raw error notification fires.

- [ ] **Step 3: Implement**

In `src/app/download/download.component.ts`, add imports:

```ts
import {
  ReauthRequest,
  buildAuthorizeUrl,
  extractReauth,
  normalizeTokenGetter,
} from '../shared/reauth';
```

Add the shared handler (near `getOptions`):

```ts
/**
 * Inspect a failed HTTP response for a re-authentication demand (structured
 * 401 {"reauth": ...} payload or legacy *scopes* marker) and trigger a new
 * Globus authorization with the demanded scopes/domains. Returns true when
 * re-auth was triggered so callers can skip regular error handling.
 */
private handleReauthError(err: unknown): boolean {
  const reauth = extractReauth(err);
  if (!reauth) return false;
  this.getRepoToken(reauth);
  return true;
}
```

Replace `getRepoToken()` with:

```ts
getRepoToken(reauth?: ReauthRequest) {
  const tg = this.globusPlugin()?.tokenGetter;
  if (tg === undefined) {
    return;
  }
  let url =
    this.globusPlugin()?.sourceUrlFieldValue +
    (tg.URL === undefined ? '' : tg.URL);
  if (tg.URL?.includes('://')) {
    url = tg.URL;
  }
  const base = normalizeTokenGetter(tg, url);
  if (base) {
    const nonce = this.newNonce(44);
    // Include all state to preserve across OAuth redirect
    const loginState: LoginState = {
      datasetId:
        this.datasetId() && this.datasetId() !== '?'
          ? { value: this.datasetId()!, label: this.datasetId()! }
          : undefined,
      nonce: nonce,
      download: true,
      downloadId: this.downloadId(),
      accessMode: this.accessMode(),
      dataverseToken: this.dataverseToken(),
      preSelectedFileIds:
        this.preSelectedFileIds().size > 0
          ? Array.from(this.preSelectedFileIds())
          : undefined,
    };
    this.navigation.assign(
      buildAuthorizeUrl(base, {
        redirectUri: this.pluginService.getRedirectUri(),
        state: JSON.stringify(loginState),
        reauth,
        // Guests/preview users may authenticate with any Globus identity —
        // unless an endpoint policy explicitly demanded a domain.
        guestMode:
          this.accessMode() === 'guest' || this.accessMode() === 'preview',
      }),
    );
  } else {
    window.open(url, '_blank');
  }
}
```

In `getOptions` (~line 1147), add the reauth check first:

```ts
error: (err) => {
  if (this.handleReauthError(err)) return;
  this.notificationService.showError(`Branch lookup failed: ${err.error}`);
  this.branchItems.set([]);
  this.option.set(undefined);
  this.optionsLoading.set(false);
},
```

In `download()` (~line 1018), add the reauth check after resetting the progress flags:

```ts
error: (err: unknown) => {
  this.downloadRequested.set(false);
  this.downloadInProgress.set(false);
  this.statusPollingActive.set(false);
  if (this.handleReauthError(err)) return;

  const fallbackError = 'unknown error';
  const message =
    (err as { error?: string; message?: string })?.error ??
    (err as { message?: string })?.message ??
    fallbackError;
  this.notificationService.showError(`Download request failed: ${message}`);
},
```

In the repo search subscription (~lines 462–479), add the check in both the promise catch and the error callback:

```ts
this.trackSubscription(
  this.repoSearchResultsObservable.subscribe({
    next: (x) =>
      x
        .then((v) => this.repoNames.set(v))
        .catch((err) => {
          if (this.handleReauthError(err)) return;
          this.repoNames.set([
            {
              label: `search failed: ${err.message}`,
              value: err.message,
            },
          ]);
        }),
    error: (err) => {
      if (this.handleReauthError(err)) return;
      this.repoNames.set([
        { label: `search failed: ${err.message}`, value: err.message },
      ]);
    },
  }),
);
```

- [ ] **Step 4: Fix the pre-existing URL-assertion specs**

`download.component.spec.ts` ~line 751 `getRepoToken strips session_required_single_domain for guest users`: the behavior is preserved (guest mode omits base domains) but the assertion may match on the raw string — re-point it at the built URL params:

```ts
const assigned = navigationSpy.assign.calls.mostRecent().args[0] as string;
expect(new URL(assigned).searchParams.get('session_required_single_domain')).toBeNull();
```

Specs at ~lines 654/724/738 assert on the assigned URL — update the same way if encoding/order differences break them (assert via `new URL(...).searchParams`, not substring matching).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: PASS — the two new regression specs green, guest-strip and URL-building specs green.

Deliberate non-wiring: the transfer-progress card's status polling (`GlobusTransferStatus`) is **not** connected to re-login. A reauth 401 there would concern an already-submitted task, and recovery for submitted transfers is out of scope per the spec's Decisions section; the card keeps showing its polling-error text.

- [ ] **Step 6: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration-frontend && git add src/app/download && git commit -m "Download: trigger Globus reauth on consent/domain errors instead of raw failures"
```

---

### Task 8: Compare-polling reauth (DataStateService)

**Files:**
- Modify: `src/app/data.state.service.ts` (`initializeState` error handler ~line 44, `getCompareData` catch ~line 100)
- Test: `src/app/data.state.service.spec.ts` (create if it does not exist; check first — `ls src/app/data.state.service.spec.ts`)

**Interfaces:**
- Consumes: `extractReauth`, `storePendingReauth` from `src/app/shared/reauth`. The connect component's ngOnInit pickup (Task 6) completes the loop.
- Produces: on a reauth error during compare submission or polling, the service stores the demand and navigates to `/connect` (no `reset` param), where Task 6's pickup re-launches OAuth.

- [ ] **Step 1: Write the failing spec**

If `src/app/data.state.service.spec.ts` does not exist, create it with the standard TestBed setup (`provideHttpClientTesting`, Router spy, NotificationService spy — mirror `src/app/shared/folder-status.service.spec.ts` for service-test structure). Add:

```ts
it('stores pending reauth and navigates to /connect when polling fails with 401 reauth', async () => {
  // Arrange: dataService.getData returns a key; getCachedData errors with:
  const reauthErr = {
    status: 401,
    error: { reauth: { required_domains: ['sydney.edu.au'] } },
  };
  // Act: service.initializeState(); flush the observables.
  // Assert:
  expect(takePendingReauth()).toEqual({ domains: ['sydney.edu.au'] });
  expect(routerSpy.navigate).toHaveBeenCalledWith(['/connect']);
});

it('keeps legacy 401 reset navigation for non-reauth 401s', async () => {
  const plainErr = { status: 401, error: '401 - session expired' };
  // Same arrangement; assert:
  expect(routerSpy.navigate).toHaveBeenCalledWith(['/connect'], {
    queryParams: { reset: 'true' },
  });
});
```

(Import `takePendingReauth` from `./shared/reauth` in the spec; clear `sessionStorage` in `afterEach`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: FAIL — service navigates with reset params / does not store reauth.

- [ ] **Step 3: Implement**

In `src/app/data.state.service.ts`, add the import:

```ts
import { extractReauth, storePendingReauth } from './shared/reauth';
```

In `initializeState`'s error handler, add the reauth branch first and harden the string check:

```ts
error: (err) => {
  const reauth = extractReauth(err);
  if (reauth) {
    storePendingReauth(reauth);
    this.notificationService.showError(
      'The repository requires re-authentication. Redirecting to login...',
    );
    this.router.navigate(['/connect']);
    this.dataSubscription = undefined;
    return;
  }
  const is401 =
    err.status === 401 ||
    (typeof err.error === 'string' && err.error.includes('401'));
  this.notificationService.showError(`Getting data failed: ${err.error}`);
  this.router.navigate(['/connect'], {
    queryParams: is401 ? { reset: 'true' } : {},
  });
  this.dataSubscription = undefined;
},
```

In `getCompareData`'s catch block, the same pattern before the existing `is401` logic:

```ts
const reauth = extractReauth(err);
if (reauth) {
  storePendingReauth(reauth);
  this.notificationService.showError(
    'The repository requires re-authentication. Redirecting to login...',
  );
  this.router.navigate(['/connect']);
  return;
}
const error = err as { status?: number; error?: string };
const is401 =
  error.status === 401 ||
  (typeof error.error === 'string' && error.error.includes('401'));
this.notificationService.showError(`Comparing failed: ${error.error}`);
this.router.navigate(['/connect'], {
  queryParams: is401 ? { reset: 'true' } : {},
});
return;
```

(The `typeof ... === 'string'` guards also fix a latent crash: with JSON error bodies, `error.error` is an object and `.includes` would throw.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration-frontend && git add src/app/data.state.service.ts src/app/data.state.service.spec.ts && git commit -m "Compare polling: hand reauth demands to connect via pending-reauth storage"
```

---

### Task 9: Documentation and full verification

**Files:**
- Modify: `GLOBUS_INTEGRATION.md` (backend repo — "Scoped Institutional Login" and "Configuration" sections)

**Interfaces:** none — documentation and verification only.

- [ ] **Step 1: Update `GLOBUS_INTEGRATION.md`**

In the "Scoped Institutional Login" section, after the existing paragraphs, add:

```markdown
### Dynamic re-authentication

The configured domain and scopes are the *initial* login requirements, not a
hard limit. When a Globus API call fails because the user's session is
missing something, the backend returns a structured response:

    HTTP/1.1 401 Unauthorized
    Content-Type: application/json

    {"reauth": {"required_scopes": ["..."], "required_domains": ["..."], "message": "..."}}

Two error classes are detected on every backend Globus call (directory
listing, endpoint lookup, endpoint search, transfer submission):

- **`ConsentRequired`** — GCS v5 mapped collections (e.g. VSC HPC, ManGO)
  require a per-collection `data_access` consent. The frontend re-runs the
  OAuth login with the required scope *merged into* the configured scopes
  (configured entries such as `openid email profile` are preserved). Globus
  consents persist per user, so this happens at most once per user and
  collection.
- **Identity-domain policies** — endpoints restricted to identities from
  specific domains (reported via `authorization_parameters` or embedded in
  GridFTP 530 text). The frontend re-runs the login with
  `session_required_single_domain=<required domains>` replacing the
  configured value, plus `prompt=login` to force authenticating the matching
  identity.

Guest and preview users log in without the configured domain restriction, but
an endpoint-demanded domain is still enforced for them.
```

In the "Configuration" section, document the structured token getter (keep the legacy example too):

```markdown
The Globus token getter supports structured fields (preferred):

    "tokenGetter": {
        "URL": "https://auth.globus.org/v2/oauth2/authorize",
        "oauth_client_id": "<client id>",
        "scopes": ["urn:globus:auth:scope:transfer.api.globus.org:all", "openid", "email", "profile"],
        "session_required_single_domain": ["kuleuven.be"]
    }

The legacy form — `scope` and `session_required_single_domain` baked into the
`URL` query string — keeps working: the frontend parses those parameters out
of the URL when the structured fields are absent.
```

(Adjust surrounding prose so the section reads naturally; do not delete existing content that is still accurate.)

- [ ] **Step 2: Full backend verification**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration/image && go vet ./app/... && go test ./app/...`
Expected: vet clean, all tests PASS.

- [ ] **Step 3: Full frontend verification**

Run: `cd /home/eryk/workspaces/redcap/rdm-integration-frontend && npm run test:ci`
Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
cd /home/eryk/workspaces/redcap/rdm-integration && git add GLOBUS_INTEGRATION.md && git commit -m "Document dynamic Globus re-authentication and structured tokenGetter config"
```

---

## Post-implementation notes (not tasks)

- **Manual smoke test** (needs real Globus accounts/endpoints, cannot be automated here): fresh user browsing a ManGO/VSC mapped collection in the download flow should get bounced to Globus consent and land back working; a user with a domain-policied destination should get bounced to an institutional login for that domain.
- **Follow-up (out of scope):** migrate `rdm-deployment/config/datasync/frontend_config.json` to the structured fields; task-event monitoring of already-submitted transfers.
