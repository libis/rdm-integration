// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func TestLoadFrontendConfigReadsTheConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontend_config.json")
	content := `{"dataverseHeader":"From file","plugins":[{"id":"only","plugin":"gitlab"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FRONTEND_CONFIG_FILE", path)
	c := LoadFrontendConfig()
	if c.DataverseHeader != "From file" || len(c.Plugins) != 1 || c.Plugins[0].Plugin != "gitlab" {
		t.Fatalf("configured file not used: %+v", c)
	}
}

func TestLoadFrontendConfigFallsBackToTheEmbeddedDefault(t *testing.T) {
	t.Setenv("FRONTEND_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.json"))
	c := LoadFrontendConfig()
	found := false
	for _, p := range c.Plugins {
		found = found || p.Plugin == "github"
	}
	if !found {
		t.Fatalf("embedded default not used: %+v", c.Plugins)
	}
}

func TestPluginTypesListsEachConfiguredTypeOnce(t *testing.T) {
	c := Configuration{Plugins: []RepoPlugin{
		{Id: "kulgitlab", Plugin: "gitlab"},
		{Id: "gitlab.com", Plugin: "gitlab"},
		{Id: "github", Plugin: "github"},
		{Id: "broken", Plugin: ""},
	}}
	if got, want := c.PluginTypes(), []string{"gitlab", "github"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDefaultFrontendConfigDoesNotEnableLocal(t *testing.T) {
	t.Setenv("FRONTEND_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.json"))
	pluginTypes := LoadFrontendConfig().PluginTypes()
	if slices.Contains(pluginTypes, "local") || !slices.Contains(pluginTypes, "github") {
		t.Errorf("unexpected plugin types in the default configuration: %v", pluginTypes)
	}
}
