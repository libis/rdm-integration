// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package options

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

func postOptions(t *testing.T, pluginName string) *httptest.ResponseRecorder {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	t.Cleanup(fr.Reset)
	body, _ := json.Marshal(types.OptionsRequest{Plugin: pluginName, PluginId: pluginName, Url: "/"})
	rec := httptest.NewRecorder()
	Options(rec, httptest.NewRequest(http.MethodPost, "/api/plugin/options", bytes.NewReader(body)))
	return rec
}

func TestOptionsRejectsAnUnconfiguredPlugin(t *testing.T) {
	fake := plugintest.Install(t, "local", "github", "gitlab")
	rec := postOptions(t, "local")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"local"`) {
		t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
	}
	if fake.Calls() != 0 {
		t.Errorf("the unconfigured plugin ran %d times", fake.Calls())
	}
}

func TestOptionsServesAConfiguredPlugin(t *testing.T) {
	fake := plugintest.Install(t, "fake", "fake")
	rec := postOptions(t, "fake")
	if rec.Code != http.StatusOK || fake.Calls() != 1 || !strings.Contains(rec.Body.String(), plugintest.Item.Label) {
		t.Errorf("configured plugin not served: %d calls, got %d %q", fake.Calls(), rec.Code, rec.Body.String())
	}
}
