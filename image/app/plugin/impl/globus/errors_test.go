// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

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
