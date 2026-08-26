// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"context"
	"integration/app/config"
	"integration/app/plugin/types"
	"integration/app/tree"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Seam tests for SaveAfterDirectUpload against a fake Dataverse: the unit
// tests on verifyBatchResults keep passing even if the call to it is dropped
// from SaveAfterDirectUpload (the pre-21bbd3 trust-all behavior), so these
// exercise the full HTTP round-trip.

func saveTwoFilesAgainstServer(t *testing.T, handler http.HandlerFunc) (map[string]bool, error) {
	t.Helper()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	prev := config.GetConfig()
	config.SetConfig(ts.URL, prev.Options.RootDataverseId, "", nil, false, prev.Options.MaxFileSize)
	defer config.SetConfig(prev.DataverseServer, prev.Options.RootDataverseId, "", nil, false, prev.Options.MaxFileSize)

	storageIdentifiers := []string{"s3://bucket:ok-file", "s3://bucket:failed-file"}
	nodes := []tree.Node{{Name: "ok.bin"}, {Name: "failed.bin"}}
	return SaveAfterDirectUpload(context.Background(), false, "token", "user", "doi:10.1/TEST", storageIdentifiers, nodes)
}

// A status "OK" response can still contain per-file failures (e.g. a file
// over the server size limit): the failed file must be reported, classified
// unrecoverable (retrying a deterministic rejection cannot succeed), and the
// registered file must not be rolled back.
func TestSaveAfterDirectUploadReportsPartialFailureUnrecoverable(t *testing.T) {
	response := `{"status":"OK","data":{"Files":[
		{"storageIdentifier":"s3://bucket:ok-file","successMessage":"added"},
		{"storageIdentifier":"s3://bucket:failed-file","errorMessage":"exceeds the size limit"}]}}`
	registered, err := saveTwoFilesAgainstServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "addFiles") {
			t.Errorf("unexpected request path: %v", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	})

	if err == nil {
		t.Fatal("expected an error for the file the server refused, got silent success")
	}
	if !types.IsUnrecoverable(err) {
		t.Error("a per-file server rejection must be unrecoverable so the job fails fast instead of grinding retries")
	}
	if !strings.Contains(err.Error(), "failed-file") || !strings.Contains(err.Error(), "exceeds the size limit") {
		t.Errorf("expected the failed identifier and server message in the error, got: %v", err)
	}
	if !registered["s3://bucket:ok-file"] {
		t.Error("the successfully registered file must not be reported as failed")
	}
	if registered["s3://bucket:failed-file"] {
		t.Error("the refused file must not be reported as registered")
	}
}

// A 200 response that is not the expected JSON (e.g. a proxy error page)
// must never be interpreted as "all files registered".
func TestSaveAfterDirectUploadGarbage200IsAnError(t *testing.T) {
	_, err := saveTwoFilesAgainstServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	})

	if err == nil {
		t.Fatal("expected an error for a non-JSON 200 response, got silent success")
	}
}
