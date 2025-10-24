// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/dataverse"
	"io"
	"net/http"
	"sync"
)

type AddFileRequest struct {
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
	FileName     string `json:"fileName"`
	Content      string `json:"content"`
}

func AddFileToDataset(w http.ResponseWriter, r *http.Request) {
	// Check if DDI-CDI feature is enabled
	if !config.IsDdiCdiEnabled() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - DDI-CDI feature is disabled"))
		return
	}

	// Process request
	req := AddFileRequest{}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	// Validate inputs
	if req.PersistentId == "" || req.FileName == "" || req.Content == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - missing required fields"))
		return
	}

	// Get user from header
	user := core.GetUserFromHeader(r.Header)

	// Upload file to Dataverse using ApiAddReplaceFile
	err = uploadFileToDataverse(r.Context(), req.PersistentId, req.DataverseKey, user, req.FileName, req.Content)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - failed to add file: %v", err)))
		return
	}

	// Return success response
	res := Key{Key: "success"}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func uploadFileToDataverse(ctx context.Context, persistentId, apiKey, user, fileName, content string) error {
	// Check if file already exists in the dataset to determine if we need to replace
	existingFileId := int64(0)

	// Get dataset nodes to check for existing file
	nodes, _, _, err := dataverse.GetDatasetNodesWithAccessInfo(ctx, persistentId, apiKey, user)
	if err == nil {
		// Look for existing file with the same name
		for _, node := range nodes {
			if node.Name == fileName && node.Attributes.DestinationFile.Id != 0 {
				existingFileId = node.Attributes.DestinationFile.Id
				break
			}
		}
	}

	// Use ApiAddReplaceFile which supports:
	// - OAuth2 authentication
	// - URL signing
	// - Streaming implementation
	// - Replace if file exists (when existingFileId != 0)
	var wg sync.WaitGroup
	asyncErr := &core.ErrorHolder{}

	writer, err := dataverse.ApiAddReplaceFile(ctx, existingFileId, fileName, apiKey, user, persistentId, &wg, asyncErr)
	if err != nil {
		return fmt.Errorf("failed to initialize file upload: %w", err)
	}

	// Write content to the writer (streaming)
	_, err = writer.Write([]byte(content))
	if err != nil {
		writer.Close()
		wg.Wait()
		return fmt.Errorf("failed to write file content: %w", err)
	}

	// Close the writer to finalize upload
	err = writer.Close()
	if err != nil {
		wg.Wait()
		return fmt.Errorf("failed to close file writer: %w", err)
	}

	// Wait for async upload to complete
	wg.Wait()

	// Check for async errors
	if asyncErr.Err != nil {
		return fmt.Errorf("upload failed: %w", asyncErr.Err)
	}

	return nil
}
