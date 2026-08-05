// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifyBatchResultsAllRegistered(t *testing.T) {
	identifiers := []string{"s3://dataverse:aaa-111", "s3://dataverse:bbb-222"}
	files := []addReplaceBatchFileResult{
		{StorageIdentifier: "s3://dataverse:aaa-111", SuccessMessage: "Added successfully to the dataset"},
		{StorageIdentifier: "s3://dataverse:bbb-222", WarningMessage: "duplicate content"},
	}
	registered, err := verifyBatchResults(files, identifiers)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	for _, id := range identifiers {
		if !registered[id] {
			t.Errorf("expected %v to be registered", id)
		}
	}
}

func TestVerifyBatchResultsPartialFailure(t *testing.T) {
	identifiers := []string{"s3://dataverse:aaa-111", "s3://dataverse:bbb-222", "s3://dataverse:ccc-333"}
	files := []addReplaceBatchFileResult{
		{StorageIdentifier: "s3://dataverse:aaa-111", SuccessMessage: "Added successfully to the dataset"},
		{StorageIdentifier: "s3://dataverse:bbb-222", ErrorMessage: "BAD_REQUEST: storage object not found"},
	}
	registered, err := verifyBatchResults(files, identifiers)
	if err == nil {
		t.Fatal("expected an error for a partial failure, got nil")
	}
	if !registered["s3://dataverse:aaa-111"] {
		t.Error("expected the successful file to be registered")
	}
	if registered["s3://dataverse:bbb-222"] || registered["s3://dataverse:ccc-333"] {
		t.Error("expected failed and unreported files to not be registered")
	}
	if !strings.Contains(err.Error(), "2 out of 3") {
		t.Errorf("expected the error to report 2 out of 3 failed files, got: %v", err)
	}
	if !strings.Contains(err.Error(), "storage object not found") {
		t.Errorf("expected the error to contain the server error message, got: %v", err)
	}
}

func TestVerifyBatchResultsLegacyResponseWithoutFileEntries(t *testing.T) {
	identifiers := []string{"s3://dataverse:aaa-111"}
	registered, err := verifyBatchResults(nil, identifiers)
	if err != nil {
		t.Fatalf("expected no error for a response without per-file entries, got: %v", err)
	}
	if !registered["s3://dataverse:aaa-111"] {
		t.Error("expected legacy behavior to trust the overall status")
	}
}

func TestAddReplaceBatchResponseUnmarshal(t *testing.T) {
	// response format as constructed by AddReplaceFileHelper.addFiles
	body := `{
		"status": "OK",
		"data": {
			"Files": [
				{
					"storageIdentifier": "s3://dataverse:aaa-111",
					"successMessage": "Added successfully to the dataset",
					"fileDetails": {"fileName": "ok.bin"}
				},
				{
					"storageIdentifier": "s3://dataverse:bbb-222",
					"errorMessage": "INTERNAL_SERVER_ERROR: Failed to add file to dataset",
					"fileDetails": {"fileName": "failed.bin"}
				}
			],
			"Result": {"Total number of files": 2, "Number of files successfully added": 1}
		}
	}`
	res := addReplaceBatchResponse{}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if res.Status != "OK" || len(res.Data.Files) != 2 {
		t.Fatalf("unexpected parse result: %+v", res)
	}
	_, err := verifyBatchResults(res.Data.Files, []string{"s3://dataverse:aaa-111", "s3://dataverse:bbb-222"})
	if err == nil {
		t.Fatal("expected an error: one of the two files failed")
	}
	if !strings.Contains(err.Error(), "1 out of 2") {
		t.Errorf("expected the error to report 1 out of 2 failed files, got: %v", err)
	}
}
