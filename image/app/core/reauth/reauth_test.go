// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

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

// The ConsentRequired example from the Globus Transfer API docs: one scope
// whose data_access dependencies, one per collection, are separated by spaces.
const twoCollectionScope = "urn:globus:auth:scope:transfer.api.globus.org:all" +
	"[*https://auth.globus.org/scopes/ea4c8cf2-d3a2-4c1a-92c2-59925fe7ac5b/data_access" +
	" *https://auth.globus.org/scopes/0357da44-c908-47e5-8351-ba5c695d11a7/data_access]"

func TestSanitizeKeepsAScopeForTwoCollections(t *testing.T) {
	safe := Sanitize(&Error{RequiredScopes: []string{twoCollectionScope}})
	if len(safe.RequiredScopes) != 1 || safe.RequiredScopes[0] != twoCollectionScope {
		t.Fatalf("two-collection scope was dropped: %+v", safe)
	}
}

func TestWriteErrorKeepsAScopeForTwoCollections(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, &Error{RequiredScopes: []string{twoCollectionScope}})
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "0357da44-c908-47e5-8351-ba5c695d11a7/data_access]") {
		t.Fatalf("body %s lost the second collection", body)
	}
}

func TestSanitizeRejectsSpacesOutsideTheDependencies(t *testing.T) {
	for _, s := range []string{
		"urn:globus:auth:scope:transfer.api.globus.org:all extra",
		"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/a/data_access] extra",
		"urn:globus:auth:scope:transfer.api.globus.org:all[ *https://auth.globus.org/scopes/a/data_access]",
		"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/a/data_access  *https://auth.globus.org/scopes/b/data_access]",
		"urn:globus:auth:scope:transfer.api.globus.org:all[*https://auth.globus.org/scopes/a/data_access",
		"urn:globus:auth:scope:transfer.api.globus.org:all]",
	} {
		if safe := Sanitize(&Error{RequiredScopes: []string{s}}); len(safe.RequiredScopes) != 0 {
			t.Fatalf("Sanitize kept %q", s)
		}
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
