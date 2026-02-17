package globus

import (
	"context"
	"encoding/json"
	"integration/app/config"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// TestGetDownloadFileIds_HappyPath verifies that GetDownloadFileIds correctly
// extracts numeric file IDs from the globusDownloadParameters response.
func TestGetDownloadFileIds_HappyPath(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/datasets/:persistentId/globusDownloadParameters" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("persistentId") != "doi:10.5072/FK2/TEST" {
			t.Fatalf("unexpected persistentId: %s", r.URL.Query().Get("persistentId"))
		}
		if r.URL.Query().Get("downloadId") != "dl-abc-123" {
			t.Fatalf("unexpected downloadId: %s", r.URL.Query().Get("downloadId"))
		}
		resp := map[string]interface{}{
			"status": "OK",
			"data": map[string]interface{}{
				"queryParameters": map[string]interface{}{
					"files": map[string]interface{}{
						"42": "endpoint/path/to/file1",
						"99": "endpoint/path/to/file2",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	ids, err := GetDownloadFileIds(context.Background(), "doi:10.5072/FK2/TEST", "token", "user", "dl-abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
	if ids[0] != 42 || ids[1] != 99 {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

// TestGetDownloadFileIds_EmptyFilesMap verifies that an empty files map
// returns an empty slice (not an error).
func TestGetDownloadFileIds_EmptyFilesMap(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "OK",
			"data": map[string]interface{}{
				"queryParameters": map[string]interface{}{
					"files": map[string]interface{}{},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	ids, err := GetDownloadFileIds(context.Background(), "doi:test", "token", "user", "dl-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 IDs, got %d", len(ids))
	}
}

// TestGetDownloadFileIds_NoFilesField verifies that a response missing the
// "files" key returns an error.
func TestGetDownloadFileIds_NoFilesField(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "OK",
			"data": map[string]interface{}{
				"queryParameters": map[string]interface{}{},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	_, err := GetDownloadFileIds(context.Background(), "doi:test", "token", "user", "dl-no-files")
	if err == nil {
		t.Fatal("expected error for missing files field, got nil")
	}
}

// TestGetDownloadFileIds_NonNumericFileId verifies that a non-numeric file ID
// in the files map returns an error.
func TestGetDownloadFileIds_NonNumericFileId(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "OK",
			"data": map[string]interface{}{
				"queryParameters": map[string]interface{}{
					"files": map[string]interface{}{
						"not-a-number": "endpoint/path",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	_, err := GetDownloadFileIds(context.Background(), "doi:test", "token", "user", "dl-bad-id")
	if err == nil {
		t.Fatal("expected error for non-numeric file ID, got nil")
	}
}

// TestGetDownloadFiles_HappyPath verifies the full flow: resolve file IDs and
// match them against the dataset file listing.
func TestGetDownloadFiles_HappyPath(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/datasets/:persistentId/globusDownloadParameters":
			resp := map[string]interface{}{
				"status": "OK",
				"data": map[string]interface{}{
					"queryParameters": map[string]interface{}{
						"files": map[string]interface{}{
							"10": "endpoint/path/to/10",
							"20": "endpoint/path/to/20",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		case "/api/v1/datasets/:persistentId/versions/:latest/files":
			resp := map[string]interface{}{
				"status": "OK",
				"data": []map[string]interface{}{
					{
						"label":          "file-a.csv",
						"directoryLabel": "data/subdir",
						"dataFile":       map[string]interface{}{"id": 10, "fileName": "file-a.csv"},
					},
					{
						"label":          "file-b.txt",
						"directoryLabel": "",
						"dataFile":       map[string]interface{}{"id": 20, "fileName": "file-b.txt"},
					},
					{
						"label":          "file-c.dat",
						"directoryLabel": "other",
						"dataFile":       map[string]interface{}{"id": 30, "fileName": "file-c.dat"},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	files, err := GetDownloadFiles(context.Background(), "doi:10.5072/FK2/TEST", "token", "user", "dl-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(files), files)
	}

	// Sort for deterministic checks
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	if files[0].Name != "file-a.csv" || files[0].Path != "data/subdir" {
		t.Fatalf("unexpected first file: %+v", files[0])
	}
	if files[1].Name != "file-b.txt" || files[1].Path != "" {
		t.Fatalf("unexpected second file: %+v", files[1])
	}
}

// TestGetDownloadFiles_EmptyDownloadId verifies that an empty downloadId
// returns nil without making any HTTP calls.
func TestGetDownloadFiles_EmptyDownloadId(t *testing.T) {

	files, err := GetDownloadFiles(context.Background(), "doi:test", "token", "user", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Fatalf("expected nil, got %+v", files)
	}
}

// TestGetDatasetFileEntries_HappyPath verifies that getDatasetFileEntries
// correctly parses the Dataverse file listing response.
func TestGetDatasetFileEntries_HappyPath(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/datasets/:persistentId/versions/:latest/files" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]interface{}{
			"status": "OK",
			"data": []map[string]interface{}{
				{
					"label":          "readme.md",
					"directoryLabel": "",
					"dataFile":       map[string]interface{}{"id": 1, "fileName": "readme.md"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	entries, err := getDatasetFileEntries(context.Background(), "doi:test", "token", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "readme.md" {
		t.Fatalf("unexpected label: %s", entries[0].Label)
	}
}

// TestGetDatasetFileEntries_NonOKStatus verifies that a non-OK status from the
// Dataverse API returns an error.
func TestGetDatasetFileEntries_NonOKStatus(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status":  "ERROR",
			"message": "not found",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config.SetConfig(server.URL, "", "", nil, false, 0)

	_, err := getDatasetFileEntries(context.Background(), "doi:test", "token", "user")
	if err == nil {
		t.Fatal("expected error for non-OK status, got nil")
	}
}
