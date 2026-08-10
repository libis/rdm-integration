// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

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
	for _, banned := range []string{`"scopes"`, `"session_required_single_domain"`} {
		if strings.Contains(string(out), banned) {
			t.Fatalf("omitempty violated, legacy config gained %s: %s", banned, out)
		}
	}
}
