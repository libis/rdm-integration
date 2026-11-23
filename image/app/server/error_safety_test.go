// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package server

import (
	"encoding/json"
	"integration/app/core/reauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveThroughBoundary(handler http.HandlerFunc) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	safeErrorResponses(handler).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/plugin/options", nil))
	return w
}

// A Globus transfer that needs consent for two collections gets one scope with
// both data_access dependencies (Globus Transfer API docs); the reauth demand
// must reach the frontend intact.
func TestErrorBoundaryKeepsAReauthForTwoCollections(t *testing.T) {
	scope := "urn:globus:auth:scope:transfer.api.globus.org:all" +
		"[*https://auth.globus.org/scopes/ea4c8cf2-d3a2-4c1a-92c2-59925fe7ac5b/data_access" +
		" *https://auth.globus.org/scopes/0357da44-c908-47e5-8351-ba5c695d11a7/data_access]"
	w := serveThroughBoundary(func(w http.ResponseWriter, r *http.Request) {
		reauth.WriteError(w, &reauth.Error{RequiredScopes: []string{scope}})
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var payload struct {
		Reauth *reauth.Error `json:"reauth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Reauth == nil {
		t.Fatalf("body %q is not a reauth payload: %v", w.Body.String(), err)
	}
	if len(payload.Reauth.RequiredScopes) != 1 || payload.Reauth.RequiredScopes[0] != scope {
		t.Fatalf("scopes = %v, want [%s]", payload.Reauth.RequiredScopes, scope)
	}
}

func TestErrorBoundaryHidesErrorDetails(t *testing.T) {
	w := serveThroughBoundary(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - upstream said token=secret-123"))
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-123") || w.Body.String() != "500 - request failed" {
		t.Fatalf("body = %q, want the fixed message", w.Body.String())
	}
}

// A collection with an authentication policy demands a session that satisfies
// it; that demand must pass the boundary like scopes and domains do.
func TestErrorBoundaryKeepsAPolicyDemand(t *testing.T) {
	policy := "6f5a1e2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	w := serveThroughBoundary(func(w http.ResponseWriter, r *http.Request) {
		reauth.WriteError(w, &reauth.Error{RequiredPolicies: []string{policy}})
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var payload struct {
		Reauth *reauth.Error `json:"reauth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Reauth == nil ||
		len(payload.Reauth.RequiredPolicies) != 1 || payload.Reauth.RequiredPolicies[0] != policy {
		t.Fatalf("body %q lost the policy demand", w.Body.String())
	}
}
