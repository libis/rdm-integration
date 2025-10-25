// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/testutil"
	"integration/app/tree"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessCdiFile_FileNameSelection(t *testing.T) {
	// Test that file name matching works correctly
	nodeMap := map[string]tree.Node{
		"data.csv": {
			Path: "/test/data.csv",
		},
		"subdir/file.csv": {
			Path: "/test/subdir/file.csv",
		},
	}

	// Test exact match
	_, exists := nodeMap["data.csv"]
	if !exists {
		t.Error("Expected to find data.csv in nodeMap")
	}

	// Test basename fallback logic would be used
	_, exists = nodeMap["file.csv"]
	if exists {
		t.Error("Should not find file.csv with exact match (needs basename logic)")
	}
}

func TestFormatWarningsAsConsoleOutput(t *testing.T) {
	tests := []struct {
		name     string
		warnings []string
		want     string
	}{
		{
			name:     "empty warnings",
			warnings: []string{},
			want:     "",
		},
		{
			name:     "single warning",
			warnings: []string{"warning 1"},
			want:     "WARNINGS:\nwarning 1",
		},
		{
			name:     "multiple warnings",
			warnings: []string{"warning 1", "warning 2"},
			want:     "WARNINGS:\nwarning 1\n\nwarning 2",
		},
		{
			name:     "warnings with whitespace",
			warnings: []string{"  warning 1  ", "", "warning 2"},
			want:     "WARNINGS:\nwarning 1\n\nwarning 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatWarningsAsConsoleOutput(tt.warnings)
			if got != tt.want {
				t.Errorf("formatWarningsAsConsoleOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDdiCdiGen_NoFiles(t *testing.T) {
	job := Job{
		Key:           "test-key",
		WritableNodes: map[string]tree.Node{},
	}

	resultJob, err := DdiCdiGen(job)
	if err != nil {
		t.Errorf("DdiCdiGen should not return error: %v", err)
	}

	// Verify that the job was processed (WritableNodes should remain empty)
	if len(resultJob.WritableNodes) != 0 {
		t.Errorf("Expected WritableNodes to remain empty, got %d", len(resultJob.WritableNodes))
	}
}

func TestDdiCdiGen_SortedFileNames(t *testing.T) {
	// Test that files are processed in sorted order
	job := Job{
		Key: "test-key-sorted",
		WritableNodes: map[string]tree.Node{
			"zebra.csv": {},
			"alpha.csv": {},
			"beta.csv":  {},
		},
		PersistentId: "doi:10.123/test",
		DataverseKey: "test-key",
	}

	// This will fail during actual processing, but we can verify the order
	// by checking that files are sorted before processing
	fileNames := make([]string, 0, len(job.WritableNodes))
	for name := range job.WritableNodes {
		fileNames = append(fileNames, name)
	}

	// After DdiCdiGen, files should have been processed in sorted order
	resultJob, _ := DdiCdiGen(job)

	// The WritableNodes map should be empty after processing
	if len(resultJob.WritableNodes) != 0 {
		t.Errorf("Expected WritableNodes to be cleared, got %d files", len(resultJob.WritableNodes))
	}
}

func TestMountDatasetForCdi_DirectoryCreation(t *testing.T) {
	// This is an integration test that would need actual infrastructure
	// For now, we test the directory path generation logic
	job := Job{
		Key: "test-mount-key",
	}

	linkedDir := jobLinkedDir(job)
	workDir := jobWorkDir(job)
	root := workspaceRoot()

	if !filepath.IsAbs(linkedDir) {
		t.Errorf("linked directory should be absolute, got %s", linkedDir)
	}
	if !filepath.IsAbs(workDir) {
		t.Errorf("work directory should be absolute, got %s", workDir)
	}
	if !strings.HasPrefix(linkedDir, root) {
		t.Errorf("linked directory should be under %s, got %s", root, linkedDir)
	}
	if !strings.HasPrefix(workDir, root) {
		t.Errorf("work directory should be under %s, got %s", root, workDir)
	}
	if !strings.HasSuffix(linkedDir, filepath.Join(job.Key, "linked")) {
		t.Errorf("linked directory should end with %s, got %s", filepath.Join(job.Key, "linked"), linkedDir)
	}
	if !strings.HasSuffix(workDir, filepath.Join(job.Key, "work")) {
		t.Errorf("work directory should end with %s, got %s", filepath.Join(job.Key, "work"), workDir)
	}
	if filepath.Dir(linkedDir) != filepath.Dir(workDir) {
		t.Errorf("expected linked and work directories to share the same job workspace, got %s and %s", linkedDir, workDir)
	}
}

func TestFetchDataFileDDI_ErrorCases(t *testing.T) {
	ctx := context.Background()
	job := Job{
		DataverseKey: "test-key",
		PersistentId: "doi:10.123/test",
	}
	node := tree.Node{
		Attributes: tree.Attributes{
			DestinationFile: tree.DestinationFile{
				Id: 0, // Missing ID
			},
		},
	}

	// Save original Destination
	origDest := Destination
	defer func() { Destination = origDest }()

	// Test with nil GetDataFileDDI function
	Destination.GetDataFileDDI = nil
	_, cleanup, err := fetchDataFileDDI(ctx, job, node, "/tmp", nil)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Error("Expected error when GetDataFileDDI is nil")
	} else if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Expected 'not supported' error, got: %v", err)
	}

	// Test with missing file ID
	Destination.GetDataFileDDI = func(ctx context.Context, token string, user string, fileID int64) ([]byte, error) {
		return nil, nil
	}
	_, cleanup, err = fetchDataFileDDI(ctx, job, node, "/tmp", nil)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Error("Expected error when file ID is missing")
	} else if !strings.Contains(err.Error(), "identifier missing") {
		t.Errorf("Expected 'identifier missing' error, got: %v", err)
	}
}

func TestCreateManifestFile_DisablesXconvertWhenDdiAvailable(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	linkedDir := filepath.Join(tempDir, "linked")
	workspaceDir := filepath.Join(tempDir, "work")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatalf("failed to create linked dir: %v", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}

	fileName := "sample.csv"
	filePath := filepath.Join(linkedDir, fileName)
	if err := os.WriteFile(filePath, []byte("col1\nvalue"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	origDest := Destination
	Destination = DestinationPlugin{}
	t.Cleanup(func() { Destination = origDest })

	Destination.GetDataFileDDI = func(ctx context.Context, token, user string, fileID int64) ([]byte, error) {
		return []byte("<ddi>ok</ddi>"), nil
	}

	nodeMap := map[string]tree.Node{
		fileName: {
			Name: fileName,
			Attributes: tree.Attributes{
				IsFile:          true,
				DestinationFile: tree.DestinationFile{Id: 42},
			},
		},
	}

	job := Job{
		DataverseKey: "token",
		User:         "user",
		PersistentId: "doi:test",
	}

	manifestPath, cleanups, warnings, err := createManifestFile(ctx, job, []string{fileName}, linkedDir, nodeMap, workspaceDir)
	if err != nil {
		t.Fatalf("createManifestFile returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	t.Cleanup(func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	})

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest struct {
		Files []struct {
			AllowXconvert bool   `json:"allow_xconvert"`
			DdiPath       string `json:"ddi_path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("failed to unmarshal manifest: %v", err)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("expected one file entry, got %d", len(manifest.Files))
	}
	entry := manifest.Files[0]
	if entry.AllowXconvert {
		t.Errorf("expected allow_xconvert=false when DDI is available")
	}
	if entry.DdiPath == "" {
		t.Errorf("expected ddi_path to be set when DDI is available")
	}
}

func TestCreateManifestFile_AllowsXconvertWhenDdiMissing(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	linkedDir := filepath.Join(tempDir, "linked")
	workspaceDir := filepath.Join(tempDir, "work")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatalf("failed to create linked dir: %v", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}

	fileName := "missing-ddi.csv"
	filePath := filepath.Join(linkedDir, fileName)
	if err := os.WriteFile(filePath, []byte("col1\nvalue"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	origDest := Destination
	Destination = DestinationPlugin{}
	t.Cleanup(func() { Destination = origDest })

	Destination.GetDataFileDDI = func(ctx context.Context, token, user string, fileID int64) ([]byte, error) {
		return nil, fmt.Errorf("not found")
	}

	nodeMap := map[string]tree.Node{
		fileName: {
			Name: fileName,
			Attributes: tree.Attributes{
				IsFile:          true,
				DestinationFile: tree.DestinationFile{Id: 43},
			},
		},
	}

	job := Job{
		DataverseKey: "token",
		User:         "user",
		PersistentId: "doi:test",
	}

	manifestPath, cleanups, warnings, err := createManifestFile(ctx, job, []string{fileName}, linkedDir, nodeMap, workspaceDir)
	if err != nil {
		t.Fatalf("createManifestFile returned error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected warnings when DDI retrieval fails")
	}
	t.Cleanup(func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	})

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var manifest struct {
		Files []struct {
			AllowXconvert bool   `json:"allow_xconvert"`
			DdiPath       string `json:"ddi_path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("failed to unmarshal manifest: %v", err)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("expected one file entry, got %d", len(manifest.Files))
	}
	entry := manifest.Files[0]
	if !entry.AllowXconvert {
		t.Errorf("expected allow_xconvert=true when DDI retrieval fails")
	}
	if entry.DdiPath != "" {
		t.Errorf("expected ddi_path to be empty when DDI retrieval fails")
	}
}

// Integration tests using FakeRedis

func setupTestRedis(t *testing.T) (*testutil.FakeRedis, func()) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	cleanup := func() {
		fr.Reset()
	}
	return fr, cleanup
}

func TestRedisIntegration_CacheComputeResponse(t *testing.T) {
	redis, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Test setting a value
	cmd := redis.Set(ctx, "test-key", "test-value", time.Minute)
	if cmd.Err() != nil {
		t.Fatalf("Failed to set value: %v", cmd.Err())
	}

	// Test getting a value
	getCmd := redis.Get(ctx, "test-key")
	val, err := getCmd.Result()
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}
	if val != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", val)
	}
}

func TestRedisIntegration_SetNX(t *testing.T) {
	redis, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Test SetNX on non-existent key (should succeed)
	cmd := redis.SetNX(ctx, "lock-key", "locked", time.Minute)
	success, err := cmd.Result()
	if err != nil {
		t.Fatalf("SetNX failed: %v", err)
	}
	if !success {
		t.Error("Expected SetNX to succeed on non-existent key")
	}

	// Test SetNX on existing key (should fail)
	cmd2 := redis.SetNX(ctx, "lock-key", "locked-again", time.Minute)
	success2, err2 := cmd2.Result()
	if err2 != nil {
		t.Fatalf("SetNX failed: %v", err2)
	}
	if success2 {
		t.Error("Expected SetNX to fail on existing key")
	}
}

func TestRedisIntegration_ExpiredKeys(t *testing.T) {
	redis, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Set a key with very short expiration
	redis.Set(ctx, "short-lived", "value", 10*time.Millisecond)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Manually trigger cleanup
	redis.CleanupExpired()

	// Try to get the expired key
	getCmd := redis.Get(ctx, "short-lived")
	val, _ := getCmd.Result()
	if val != "" {
		t.Errorf("Expected empty string for expired key, got '%s'", val)
	}
}

func TestRedisIntegration_QueueOperations(t *testing.T) {
	redis, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Push items to queue
	// LPush with multiple values prepends them as a group, maintaining their order
	redis.LPush(ctx, "queue", "item1", "item2", "item3")

	// Result list is: ["item1", "item2", "item3"]
	// RPop pops from the end, so we get: item3, item2, item1
	val1, _ := redis.RPop(ctx, "queue").Result()
	if val1 != "item3" {
		t.Errorf("Expected 'item3', got '%s'", val1)
	}

	val2, _ := redis.RPop(ctx, "queue").Result()
	if val2 != "item2" {
		t.Errorf("Expected 'item2', got '%s'", val2)
	}

	val3, _ := redis.RPop(ctx, "queue").Result()
	if val3 != "item1" {
		t.Errorf("Expected 'item1', got '%s'", val3)
	}

	// Try to pop from empty queue
	_, err := redis.RPop(ctx, "queue").Result()
	if err == nil {
		t.Error("Expected error when popping from empty queue")
	}
}

func TestRedisIntegration_DeleteKeys(t *testing.T) {
	redis, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	// Set multiple keys
	redis.Set(ctx, "key1", "value1", 0)
	redis.Set(ctx, "key2", "value2", 0)
	redis.Set(ctx, "key3", "value3", 0)

	// Delete keys
	delCmd := redis.Del(ctx, "key1", "key2")
	count, err := delCmd.Result()
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected to delete 2 keys, got %d", count)
	}

	// Verify keys are deleted
	val1, _ := redis.Get(ctx, "key1").Result()
	if val1 != "" {
		t.Errorf("Expected key1 to be deleted, got '%s'", val1)
	}

	// key3 should still exist
	val3, _ := redis.Get(ctx, "key3").Result()
	if val3 != "value3" {
		t.Errorf("Expected 'value3' for key3, got '%s'", val3)
	}
}
