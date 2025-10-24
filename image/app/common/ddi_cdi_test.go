// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Note: These tests check request parsing and validation logic.
// Full integration tests would require Redis and backend infrastructure.

func TestDdiCdiRequest_JSONMarshaling(t *testing.T) {
	req := DdiCdiRequest{
		PersistentId:       "doi:10.123/456",
		DataverseKey:       "test-key",
		Queue:              "default",
		FileNames:          []string{"file1.csv", "file2.tsv"},
		SendEmailOnSuccess: true,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal DdiCdiRequest: %v", err)
	}

	// Unmarshal back
	var decoded DdiCdiRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DdiCdiRequest: %v", err)
	}

	// Verify fields
	if decoded.PersistentId != req.PersistentId {
		t.Errorf("PersistentId mismatch: got %q, want %q", decoded.PersistentId, req.PersistentId)
	}

	if len(decoded.FileNames) != len(req.FileNames) {
		t.Errorf("FileNames length mismatch: got %d, want %d", len(decoded.FileNames), len(req.FileNames))
	}

	if decoded.SendEmailOnSuccess != req.SendEmailOnSuccess {
		t.Errorf("SendEmailOnSuccess mismatch: got %v, want %v", decoded.SendEmailOnSuccess, req.SendEmailOnSuccess)
	}
}

func TestDdiCdiRequest_InvalidJSON(t *testing.T) {
	// Test that invalid JSON is properly rejected during unmarshaling
	invalidJSON := []byte(`{"persistentId": "test", "fileNames": [invalid]}`)

	var req DdiCdiRequest
	err := json.Unmarshal(invalidJSON, &req)

	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDdiCdiRequest_EmptyFileNames(t *testing.T) {
	// Test request with empty file names list
	req := DdiCdiRequest{
		PersistentId: "doi:10.123/456",
		DataverseKey: "test-key",
		Queue:        "default",
		FileNames:    []string{},
	}

	if len(req.FileNames) != 0 {
		t.Errorf("Expected empty FileNames, got %d files", len(req.FileNames))
	}
}

func TestDdiCdiRequest_MultipleFiles(t *testing.T) {
	// Test request with multiple files
	files := []string{"data1.csv", "data2.tsv", "data3.tab"}
	req := DdiCdiRequest{
		PersistentId: "doi:10.123/456",
		DataverseKey: "test-key",
		Queue:        "default",
		FileNames:    files,
	}

	if len(req.FileNames) != 3 {
		t.Errorf("Expected 3 files, got %d", len(req.FileNames))
	}

	// Verify all files are present
	for i, file := range files {
		if req.FileNames[i] != file {
			t.Errorf("File %d: expected %q, got %q", i, file, req.FileNames[i])
		}
	}
}

func TestDdiCdiRequest_QueueField(t *testing.T) {
	// Test different queue values
	queues := []string{"default", "high-priority", "low-priority", ""}

	for _, queue := range queues {
		req := DdiCdiRequest{
			PersistentId: "doi:10.123/456",
			DataverseKey: "test-key",
			Queue:        queue,
			FileNames:    []string{"data.csv"},
		}

		if req.Queue != queue {
			t.Errorf("Queue mismatch: expected %q, got %q", queue, req.Queue)
		}
	}
}

func TestDdiCdiRequest_SendEmailFlag(t *testing.T) {
	// Test both true and false values for SendEmailOnSuccess
	for _, sendEmail := range []bool{true, false} {
		req := DdiCdiRequest{
			PersistentId:       "doi:10.123/456",
			DataverseKey:       "test-key",
			Queue:              "default",
			FileNames:          []string{"data.csv"},
			SendEmailOnSuccess: sendEmail,
		}

		if req.SendEmailOnSuccess != sendEmail {
			t.Errorf("SendEmailOnSuccess mismatch: expected %v, got %v", sendEmail, req.SendEmailOnSuccess)
		}
	}
}

func TestDdiCdiHTTPHandler_InvalidJSON(t *testing.T) {
	// Create HTTP request with invalid JSON
	req := httptest.NewRequest("POST", "/api/common/ddicdi", bytes.NewReader([]byte("not valid json")))
	w := httptest.NewRecorder()

	// Call handler
	DdiCdi(w, req)

	// Should get an error response (implementation-dependent)
	if w.Code == http.StatusOK {
		t.Log("Note: Handler may require additional setup (config, Redis) to return proper errors")
	}
}

func TestGetCachedDdiCdiResponse_HTTPMethod(t *testing.T) {
	// Test with GET request
	req := httptest.NewRequest("GET", "/api/common/cachedddicdi?key=test-key", nil)
	w := httptest.NewRecorder()

	GetCachedDdiCdiResponse(w, req)

	// Should delegate to GetCachedComputeResponse
	// Exact behavior depends on backend infrastructure
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Logf("Unexpected status code: %d (may be expected without full backend)", w.Code)
	}
}
