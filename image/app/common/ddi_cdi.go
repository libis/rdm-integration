// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/tree"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type DdiCdiRequest struct {
	PersistentId       string   `json:"persistentId"`
	DataverseKey       string   `json:"dataverseKey"`
	Queue              string   `json:"queue"`
	FileNames          []string `json:"fileNames"` // List of CSV/data files to process
	SendEmailOnSuccess bool     `json:"sendEmailOnSuccess"`
}

func DdiCdi(w http.ResponseWriter, r *http.Request) {
	if !config.IsDdiCdiEnabled() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - DDI-CDI feature is disabled"))
		return
	}
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := DdiCdiRequest{}
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

	// Build writable nodes map from file names
	writableNodes := make(map[string]tree.Node)
	if len(req.FileNames) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - no files specified"))
		return
	}
	for _, fileName := range req.FileNames {
		writableNodes[fileName] = tree.Node{}
	}

	key := uuid.New().String()
	res := Key{Key: key}
	core.CacheComputeResponse(core.CachedComputeResponse{
		Key:          key,
		Ready:        false,
		ConsoleOut:   "processing DDI-CDI generation...",
		ErrorMessage: "",
	})
	err = core.AddJob(r.Context(), core.Job{
		DataverseKey:       req.DataverseKey,
		User:               core.GetUserFromHeader(r.Header),
		SessionId:          core.GetSessionId(r.Header),
		PersistentId:       req.PersistentId,
		WritableNodes:      writableNodes,
		Plugin:             "ddi_cdi",
		SendEmailOnSuccess: req.SendEmailOnSuccess,
		Key:                key,
		Queue:              req.Queue,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - failed to add job: " + err.Error()))
		return
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

// GetCachedDdiCdiResponse retrieves the cached DDI-CDI generation result
// Uses the same cache structure as compute, so we can reuse GetCachedComputeResponse
// This function is provided for semantic clarity but delegates to the shared implementation
func GetCachedDdiCdiResponse(w http.ResponseWriter, r *http.Request) {
	if !config.IsDdiCdiEnabled() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - DDI-CDI feature is disabled"))
		return
	}
	GetCachedComputeResponse(w, r)
}

type GetCachedDdiCdiOutputRequest struct {
	PersistentId string `json:"persistentId"`
}

// GetCachedDdiCdiOutput retrieves the cached DDI-CDI output by dataset persistent ID
// This allows users to retrieve previously generated DDI-CDI metadata
func GetCachedDdiCdiOutput(w http.ResponseWriter, r *http.Request) {
	if !config.IsDdiCdiEnabled() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - DDI-CDI feature is disabled"))
		return
	}
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}

	req := GetCachedDdiCdiOutputRequest{}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - bad request"))
		return
	}

	if req.PersistentId == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - persistentId is required"))
		return
	}

	cacheData, err := core.GetCachedDdiCdiOutput(req.PersistentId)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - no cached output found"))
		return
	}

	responseData, err := json.Marshal(cacheData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseData)
}

// GetDdiCdiCompatibleFiles returns files that are compatible with DDI-CDI generation
// based on their file extension. Compatible formats include CSV, TSV, TAB (Dataverse tabular),
// and converted formats (SPS, SAS, DCT via xconvert).
func GetDdiCdiCompatibleFiles(w http.ResponseWriter, r *http.Request) {
	if !config.IsDdiCdiEnabled() {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - DDI-CDI feature is disabled"))
		return
	}
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}

	// Process request
	req := CompareRequest{}
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

	// Define supported extensions for DDI-CDI generation
	supported := map[string]bool{
		"csv": true, // Comma-separated values
		"tsv": true, // Tab-separated values
		"tab": true, // Dataverse tabular format
		"sps": true, // SPSS (via xconvert)
		"sas": true, // SAS (via xconvert)
		"dct": true, // Stata (via xconvert)
	}

	// Get dataset files
	user := core.GetUserFromHeader(r.Header)
	nm, err := core.Destination.Query(r.Context(), req.PersistentId, req.DataverseKey, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - getting files failed"))
		return
	}

	// Filter files by supported extensions
	data := []tree.Node{}
	for _, node := range nm {
		if node.Attributes.IsFile {
			ext := strings.TrimPrefix(filepath.Ext(node.Name), ".")
			if supported[ext] {
				data = append(data, node)
			}
		}
	}

	res := core.CompareResponse{
		Id:     req.PersistentId,
		Status: core.Finished,
		Data:   data,
		Url:    core.Destination.GetRepoUrl(req.PersistentId, false),
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
