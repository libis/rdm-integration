// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"integration/app/common"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin/plugintest"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
)

// postCompare names a new dataset, so the compare asks the destination for
// neither permissions nor files and the plugin's Query is all that runs.
func postCompare(t *testing.T, pluginName string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(types.CompareRequest{
		Plugin:       pluginName,
		PluginId:     pluginName,
		Url:          "/",
		PersistentId: "doi:10.5072/FK2/" + types.NewDataset,
	})
	rec := httptest.NewRecorder()
	Compare(rec, httptest.NewRequest(http.MethodPost, "/api/plugin/compare", bytes.NewReader(body)))
	return rec
}

func useFakeRedisAndDestination(t *testing.T) *testutil.FakeRedis {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	saved := core.Destination
	core.Destination.GetRepoUrl = func(string, bool) string { return "" }
	t.Cleanup(func() {
		core.Destination = saved
		fr.Reset()
	})
	return fr
}

func TestCompareRejectsAnUnconfiguredPlugin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		useFakeRedisAndDestination(t)
		fake := plugintest.Install(t, "local", "github", "gitlab")
		rec := postCompare(t, "local")
		synctest.Wait()
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"local"`) {
			t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
		}
		if fake.Calls() != 0 {
			t.Errorf("the unconfigured plugin ran %d times", fake.Calls())
		}
	})
}

func TestCompareServesAConfiguredPlugin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := useFakeRedisAndDestination(t)
		fake := plugintest.Install(t, "fake", "fake")
		rec := postCompare(t, "fake")
		synctest.Wait()
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d %q", rec.Code, rec.Body.String())
		}
		key := common.Key{}
		if err := json.Unmarshal(rec.Body.Bytes(), &key); err != nil {
			t.Fatal(err)
		}
		cached := common.CachedResponse{}
		if err := json.Unmarshal([]byte(fr.Get(context.Background(), key.Key).Val()), &cached); err != nil {
			t.Fatal(err)
		}
		if fake.Calls() != 1 || cached.ErrorMessage != "" || len(cached.Response.Data) != 1 || cached.Response.Data[0].Id != plugintest.Node.Id {
			t.Errorf("configured plugin not served: %d calls, cached %+v", fake.Calls(), cached)
		}
	})
}
