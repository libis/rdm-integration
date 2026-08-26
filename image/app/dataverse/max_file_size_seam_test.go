// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"context"
	"integration/app/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getMaxFileSizeAgainstServer(t *testing.T, unblockKey string, handler http.HandlerFunc) int64 {
	t.Helper()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	prev := config.GetConfig()
	prevKey := config.UnblockKey
	config.SetConfig(ts.URL, prev.Options.RootDataverseId, "", nil, false, 0)
	config.UnblockKey = unblockKey
	defer func() {
		config.SetConfig(prev.DataverseServer, prev.Options.RootDataverseId, "", nil, false, prev.Options.MaxFileSize)
		config.UnblockKey = prevKey
	}()
	return GetMaxFileSize(context.Background())
}

// Dataverse 6.7.1 does not expose :MaxFileUploadSizeInBytes on the public
// info endpoint (404): the limit must still be picked up through the admin
// endpoint using the unblock key, so the compare pre-check stays in sync with
// the server and over-limit files are rejected visibly at compare time.
func TestGetMaxFileSizeFallsBackToAdminEndpoint(t *testing.T) {
	size := getMaxFileSizeAgainstServer(t, "test-unblock", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/info/settings/") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/settings/") {
			if r.URL.Query().Get("unblock-key") != "test-unblock" {
				http.Error(w, "unauthorized", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"OK","data":{"message":"70000000000"}}`))
			return
		}
		t.Errorf("unexpected request path: %v", r.URL.Path)
	})

	if size != 70000000000 {
		t.Errorf("expected the admin-endpoint fallback to report the server limit 70000000000, got %v", size)
	}
}

// When the server limit cannot be determined at all (both endpoints
// unavailable) and no local limit is configured, uploads deliberately stay
// unlimited (fail-open): the server still enforces its own limit at
// registration, the dashboard just cannot pre-check it.
func TestGetMaxFileSizeUnknownServerLimitFailsOpen(t *testing.T) {
	size := getMaxFileSizeAgainstServer(t, "test-unblock", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if size != 0 {
		t.Errorf("expected 0 (no limit) when the server limit is unavailable, got %v", size)
	}
}
