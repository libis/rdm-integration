// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/dataverse"
	"io"
	"net/http"
)

type AddFileRequest struct {
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
	FileName     string `json:"fileName"`
	Content      string `json:"content"`
	MimeType     string `json:"mimeType"`
	Description  string `json:"description"`
}

type AddFileResponse struct {
	FileId int64  `json:"fileId"`
	Key    string `json:"key"`
}

// DDI-CDI MIME type constant
const DdiCdiMimeType = `application/ld+json;profile="http://www.w3.org/ns/json-ld#flattened http://www.w3.org/ns/json-ld#compacted https://ddialliance.org/specification/ddi-cdi/1.0"`

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

	// Use DDI-CDI MIME type if not specified
	mimeType := req.MimeType
	if mimeType == "" {
		mimeType = DdiCdiMimeType
	}

	// Use default description if not specified
	description := req.Description
	if description == "" {
		description = "DDI-CDI metadata file"
	}

	// Upload file to Dataverse and get file ID
	fileId, err := dataverse.AddFileWithMimeType(r.Context(), req.PersistentId, req.DataverseKey, user, req.FileName, req.Content, mimeType, description)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - failed to add file: %v", err)))
		return
	}

	// Return response with file ID
	res := AddFileResponse{FileId: fileId, Key: "success"}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
