// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package search

import (
	"bytes"
	"encoding/json"
	"integration/app/config"
	"integration/app/plugin/plugintest"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postSearch(t *testing.T, pluginName string) *httptest.ResponseRecorder {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	t.Cleanup(fr.Reset)
	body, _ := json.Marshal(types.OptionsRequest{Plugin: pluginName, PluginId: pluginName, Url: "/", RepoName: "fak"})
	rec := httptest.NewRecorder()
	Search(rec, httptest.NewRequest(http.MethodPost, "/api/plugin/search", bytes.NewReader(body)))
	return rec
}

func TestSearchRejectsAnUnconfiguredPlugin(t *testing.T) {
	fake := plugintest.Install(t, "local", "github", "gitlab")
	rec := postSearch(t, "local")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"local"`) {
		t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
	}
	if fake.Calls() != 0 {
		t.Errorf("the unconfigured plugin ran %d times", fake.Calls())
	}
}

func TestSearchServesAConfiguredPlugin(t *testing.T) {
	fake := plugintest.Install(t, "fake", "fake")
	rec := postSearch(t, "fake")
	if rec.Code != http.StatusOK || fake.Calls() != 1 || !strings.Contains(rec.Body.String(), plugintest.Item.Label) {
		t.Errorf("configured plugin not served: %d calls, got %d %q", fake.Calls(), rec.Code, rec.Body.String())
	}
}
