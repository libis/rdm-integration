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

// Globus CLI tests: a collection with an authentication policy answers ls
// with AuthPolicyFailed and the policy ids as a comma-separated string.
const authPolicyFailedBody = `{"authorization_parameters":{"session_message":"Failing collection authentication policy","session_required_policies":"6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b,7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"},"code":"AuthPolicyFailed","message":"Failing collection authentication policy","request_id":"MSbPbMR9n","resource":"/operation/endpoint/x/ls"}`

const policiesListBody = `{"code":"AuthPolicyFailed","message":"policy","authorization_parameters":{"session_required_policies":["6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"]}}`

const requiredIdentitiesBody = `{"code":"permission_denied","message":"identity","authorization_parameters":{"session_required_identities":["11111111-2222-4333-8444-555555555555"]}}`

const domainWithMFABody = `{"code":"permission_denied","message":"mfa","authorization_parameters":{"session_required_single_domain":["kuleuven.be"],"session_required_mfa":true}}`

const gareOnlyConsentBody = `{"code":"ConsentRequired","message":"Missing required data_access consent","authorization_parameters":{"required_scopes":["urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/00000000-0000-0000-0000-000000000002/data_access]"]}}`

// A cut-off embedded result is not valid JSON; the domain is still found.
const gridFtpTruncatedBody = `{"code":"ExternalError.DirListingFailed.LoginFailed","message":"Login Failed\n530-GridFTP-JSON-Result: {\"authorization_parameters\": {\"session_required_single_domain\": [\"sydney.edu.au\"]","request_id":"xyz"}`

const gridFtpEmbeddedPoliciesBody = `{"code":"ExternalError.DirListingFailed.LoginFailed","message":"Login Failed\n530-Login incorrect. : GlobusError: v=1 c=LOGIN_DENIED\n530-GridFTP-JSON-Result: {\"DATA_TYPE\": \"result#1.1.0\", \"authorization_parameters\": {\"session_required_policies\": [\"6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b\"]}, \"code\": \"permission_denied\", \"http_response_code\": 403}\n530 End.","request_id":"xyz"}`

// From the Globus Transfer API docs: a transfer that needs consent for two
// collections gets one scope with both data_access dependencies.
const twoCollectionScope = "urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/ea4c8cf2-d3a2-4c1a-92c2-59925fe7ac5b/data_access *https://auth.globus.org/scopes/0357da44-c908-47e5-8351-ba5c695d11a7/data_access]"

const twoCollectionConsentBody = `{"code":"ConsentRequired","message":"Missing required data_access consent","request_id":"WmMV97A1w","required_scopes":["` + twoCollectionScope + `"],"resource":"/transfer"}`

// The Globus Auth Requirements Error form (as in the Globus CLI tests): the
// scopes are repeated under authorization_parameters.
const gareConsentBody = `{"authorization_parameters":{"required_scopes":["urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/00000000-0000-0000-0000-000000000001/data_access]"],"session_message":"Missing required data_access consent"},"code":"ConsentRequired","message":"Missing required data_access consent","request_id":"m8RfX3cES","required_scopes":["urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/00000000-0000-0000-0000-000000000001/data_access]"],"resource":"/operation/endpoint/00000000-0000-0000-0000-000000000001/ls"}`

func TestInterpretGlobusError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantScopes     []string
		wantDomains    []string
		wantPolicies   []string
		wantIdentities []string
		wantMFA        bool
		wantPlain      string // expected error text when no reauth expected
	}{
		{
			name:       "consent_required_becomes_scopes",
			status:     403,
			body:       consentRequiredBody,
			wantScopes: []string{"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/82c495cc-aef8-40ad-88df-f9c92bee82d3/data_access]"},
		},
		{
			name:       "consent_for_two_collections_keeps_both",
			status:     403,
			body:       twoCollectionConsentBody,
			wantScopes: []string{twoCollectionScope},
		},
		{
			name:       "gare_consent_required_becomes_scopes",
			status:     403,
			body:       gareConsentBody,
			wantScopes: []string{"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/00000000-0000-0000-0000-000000000001/data_access]"},
		},
		{
			name:         "auth_policy_failed_becomes_policies",
			status:       403,
			body:         authPolicyFailedBody,
			wantPolicies: []string{"6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b", "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"},
		},
		{
			name:         "policies_list_becomes_policies",
			status:       403,
			body:         policiesListBody,
			wantPolicies: []string{"6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"},
		},
		{
			name:           "required_identities_become_identities",
			status:         403,
			body:           requiredIdentitiesBody,
			wantIdentities: []string{"11111111-2222-4333-8444-555555555555"},
		},
		{
			name:        "domain_with_mfa_keeps_both",
			status:      403,
			body:        domainWithMFABody,
			wantDomains: []string{"kuleuven.be"},
			wantMFA:     true,
		},
		{
			name:       "gare_only_consent_uses_authorization_parameters_scopes",
			status:     403,
			body:       gareOnlyConsentBody,
			wantScopes: []string{"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/00000000-0000-0000-0000-000000000002/data_access]"},
		},
		{
			name:        "gridftp_truncated_json_still_finds_domains",
			status:      502,
			body:        gridFtpTruncatedBody,
			wantDomains: []string{"sydney.edu.au"},
		},
		{
			name:         "gridftp_embedded_policies_become_policies",
			status:       502,
			body:         gridFtpEmbeddedPoliciesBody,
			wantPolicies: []string{"6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"},
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
			// The folder-landing loop needs the code; the message stays out
			name:      "not_found_keeps_only_the_code",
			status:    404,
			body:      notFoundBody,
			wantPlain: "ClientError.NotFound",
		},
		{
			name:      "non_json_body_keeps_only_the_status",
			status:    502,
			body:      "<html>bad gateway</html>",
			wantPlain: "globus request failed with status 502",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := interpretGlobusError(tt.status, []byte(tt.body))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			re := reauth.FromError(err)
			if len(tt.wantScopes) == 0 && len(tt.wantDomains) == 0 && len(tt.wantPolicies) == 0 &&
				len(tt.wantIdentities) == 0 && !tt.wantMFA {
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
			if strings.Join(re.RequiredPolicies, ",") != strings.Join(tt.wantPolicies, ",") {
				t.Fatalf("policies = %v, want %v", re.RequiredPolicies, tt.wantPolicies)
			}
			if strings.Join(re.RequiredIdentities, ",") != strings.Join(tt.wantIdentities, ",") {
				t.Fatalf("identities = %v, want %v", re.RequiredIdentities, tt.wantIdentities)
			}
			if re.RequiredMFA != tt.wantMFA {
				t.Fatalf("mfa = %v, want %v", re.RequiredMFA, tt.wantMFA)
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
