// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"integration/app/config"
)

func dvObjectsAgainstServer(t *testing.T, searchTerm, collection string, handler http.HandlerFunc) ([]string, error) {
	t.Helper()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	prev := config.GetConfig()
	config.SetConfig(ts.URL, prev.Options.RootDataverseId, "", []int{6, 7}, false, prev.Options.MaxFileSize)
	defer config.SetConfig(prev.DataverseServer, prev.Options.RootDataverseId, "", prev.Options.MyDataRoleIds, false, prev.Options.MaxFileSize)

	items, err := DvObjects(context.Background(), "Dataset", collection, searchTerm, "some-token", "")
	labels := []string{}
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels, err
}

// Dataverse (6.7.1 and earlier) reports a search that matches none of the
// user's objects as {"success":false,"error_message":"Sorry, no results were
// found."}. That is an empty result, not a failure: the dropdown must show no
// options instead of surfacing an error toast (the connect-page search 500).
func TestDvObjectsNoResultsIsEmptyNotError(t *testing.T) {
	labels, err := dvObjectsAgainstServer(t, "nothing-matches-this", "", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/mydata/retrieve") {
			t.Errorf("unexpected request path: %v", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"error_message":"Sorry, no results were found."}`))
	})
	if err != nil {
		t.Errorf("a no-results search must not error, got: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("a no-results search must return no items, got: %v", labels)
	}
}

// Genuine mydata failures (e.g. Solr down) must still surface as errors, not
// silently pretend the user has no datasets.
func TestDvObjectsRealFailureStillErrors(t *testing.T) {
	_, err := dvObjectsAgainstServer(t, "anything", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"error_message":"Sorry, the search service is temporarily unavailable."}`))
	})
	if err == nil {
		t.Error("a real mydata failure must be reported as an error")
	}
	if err != nil && !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Errorf("the error must carry the Dataverse message for the logs, got: %v", err)
	}
}

// A standard Dataverse API error ({"status":"ERROR","message":...}) has no
// success/error_message fields, so it used to surface as "was not successful: "
// with the cause lost. The raw response must be preserved in the error.
func TestDvObjectsStandardApiErrorKeepsRawBody(t *testing.T) {
	_, err := dvObjectsAgainstServer(t, "anything", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"ERROR","message":"Bad signed URL"}`))
	})
	if err == nil {
		t.Fatal("a standard API error must be reported as an error")
	}
	if !strings.Contains(err.Error(), "Bad signed URL") {
		t.Errorf("the error must include the raw Dataverse response, got: %v", err)
	}
}

// A non-JSON response (e.g. an HTML error page from a proxy) must not vanish
// into a bare unmarshal error: the error must show what was actually received.
func TestDvObjectsHtmlErrorKeepsRawBody(t *testing.T) {
	_, err := dvObjectsAgainstServer(t, "anything", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`<html><body><h1>502 Bad Gateway</h1></body></html>`))
	})
	if err == nil {
		t.Fatal("an HTML error response must be reported as an error")
	}
	if !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Errorf("the error must include the raw response body, got: %v", err)
	}
}

// A search term must reach mydata even when a collection filter is active;
// the old code silently replaced the typed term with the collection filter.
func TestDvObjectsSearchTermCombinesWithCollection(t *testing.T) {
	var gotSearchTerm string
	_, err := dvObjectsAgainstServer(t, "flood data", "myCollection", func(w http.ResponseWriter, r *http.Request) {
		gotSearchTerm = r.URL.Query().Get("mydata_search_term")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"data":{"pagination":{"hasNextPageNumber":false},"items":[{"name":"Flood data","global_id":"doi:10.1/AB"}]}}`))
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	// r.URL.Query().Get already URL-decodes, like Dataverse does server-side.
	decoded := gotSearchTerm
	if !strings.Contains(decoded, `text:"flood data"`) {
		t.Errorf("the typed search term must be part of the mydata query, got: %q", decoded)
	}
	if !strings.Contains(decoded, "identifierOfDataverse:(+myCollection)") {
		t.Errorf("the collection filter must be part of the mydata query, got: %q", decoded)
	}
}
