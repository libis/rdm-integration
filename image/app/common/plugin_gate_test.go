// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package common

import (
	"bytes"
	"context"
	"encoding/json"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin/plugintest"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"integration/app/tree"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const storePid = "doi:10.5072/FK2/STORE"

func postStore(t *testing.T, pluginName string) (*httptest.ResponseRecorder, *testutil.FakeRedis) {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	saved := core.Destination
	core.Destination.GetRepoUrl = func(string, bool) string { return "https://dataverse.example/dataset" }
	t.Cleanup(func() {
		core.Destination = saved
		fr.Reset()
	})
	body, _ := json.Marshal(StoreRequest{
		Plugin:        pluginName,
		StreamParams:  types.StreamParams{PluginId: pluginName, Url: "/"},
		PersistentId:  storePid,
		SelectedNodes: []tree.Node{{Id: "etc/passwd", Name: "passwd", Attributes: tree.Attributes{IsFile: true}}},
	})
	rec := httptest.NewRecorder()
	Store(rec, httptest.NewRequest(http.MethodPost, "/api/common/store", bytes.NewReader(body)))
	return rec, fr
}

func queuedJob(fr *testutil.FakeRedis) (core.Job, bool) {
	job := core.Job{}
	v := fr.RPop(context.Background(), "jobs").Val()
	if v == "" {
		return job, false
	}
	return job, json.Unmarshal([]byte(v), &job) == nil
}

func TestStoreRejectsAnUnconfiguredPluginBeforeQueueing(t *testing.T) {
	plugintest.Install(t, "local", "github", "gitlab")
	rec, fr := postStore(t, "local")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"local"`) {
		t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
	}
	if _, ok := queuedJob(fr); ok {
		t.Error("a job was queued for the unconfigured plugin")
	}
	if fr.Get(context.Background(), "lock: "+storePid).Val() != "" {
		t.Error("the dataset was locked for the unconfigured plugin")
	}
}

// Job types that are not registry plugins are queued only by their own
// handlers, never by a store request that names them.
func TestStoreRejectsJobTypesThatAreNotPlugins(t *testing.T) {
	for _, name := range []string{"compute", "ddi_cdi", "hash-only"} {
		t.Run(name, func(t *testing.T) {
			plugintest.Install(t, "fake", "fake")
			rec, fr := postStore(t, name)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d %q", rec.Code, rec.Body.String())
			}
			if _, ok := queuedJob(fr); ok {
				t.Errorf("a %s job was queued through the store handler", name)
			}
		})
	}
}

func TestStoreQueuesAJobForAConfiguredPlugin(t *testing.T) {
	plugintest.Install(t, "fake", "fake")
	rec, fr := postStore(t, "fake")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %q", rec.Code, rec.Body.String())
	}
	job, ok := queuedJob(fr)
	if !ok || job.Plugin != "fake" || job.PersistentId != storePid {
		t.Errorf("no job queued for the configured plugin: %+v", job)
	}
}

// fakeDataverse answers the user lookup that the metadata handler makes before
// it asks the plugin, so a test can see whether the plugin is reached.
func fakeDataverse(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/:me" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"status":"OK","data":{"firstName":"Ada","lastName":"Lovelace"}}`))
	}))
	prev, prevQuit := config.GetConfig(), config.AllowQuit
	config.SetConfig(srv.URL, prev.Options.RootDataverseId, prev.Options.DefaultHash, prev.Options.MyDataRoleIds, prevQuit, prev.Options.MaxFileSize)
	t.Cleanup(func() {
		config.SetConfig(prev.DataverseServer, prev.Options.RootDataverseId, prev.Options.DefaultHash, prev.Options.MyDataRoleIds, prevQuit, prev.Options.MaxFileSize)
		srv.Close()
	})
}

func postMetadata(t *testing.T, req types.GetMetadataRequest) *httptest.ResponseRecorder {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	t.Cleanup(fr.Reset)
	body, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	GetMetadata(rec, httptest.NewRequest(http.MethodPost, "/api/common/metadata", bytes.NewReader(body)))
	return rec
}

func TestMetadataRejectsAnUnconfiguredPlugin(t *testing.T) {
	fakeDataverse(t)
	fake := plugintest.Install(t, "local", "github", "gitlab")
	rec := postMetadata(t, types.GetMetadataRequest{Plugin: "local", PluginId: "local", Url: "/", Token: "token"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"local"`) {
		t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
	}
	if fake.Calls() != 0 {
		t.Errorf("the unconfigured plugin ran %d times", fake.Calls())
	}
}

// The dataverse metadata copy is handled before the generic plugin code, and
// it must not contact the requested source when dataverse is not configured.
func TestMetadataRejectsAnUnconfiguredDataverseSource(t *testing.T) {
	var hits atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"status":"OK","data":{"metadataBlocks":{}}}`))
	}))
	defer source.Close()
	plugintest.Install(t, "fake", "fake")
	rec := postMetadata(t, types.GetMetadataRequest{Plugin: "dataverse", PluginId: "dataverse", Url: source.URL, RepoName: "doi:10.5072/FK2/SRC", Token: "token"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"dataverse"`) {
		t.Errorf("expected a 400 that names the plugin, got %d %q", rec.Code, rec.Body.String())
	}
	if hits.Load() != 0 {
		t.Errorf("the source was contacted %d times", hits.Load())
	}
}

func TestMetadataServesAConfiguredPlugin(t *testing.T) {
	fakeDataverse(t)
	fake := plugintest.Install(t, "fake", "fake")
	rec := postMetadata(t, types.GetMetadataRequest{Plugin: "fake", PluginId: "fake", Url: "/", Token: "token"})
	if rec.Code != http.StatusOK || fake.Calls() != 1 || !strings.Contains(rec.Body.String(), plugintest.Title) {
		t.Errorf("configured plugin not served: %d calls, got %d %q", fake.Calls(), rec.Code, rec.Body.String())
	}
}
