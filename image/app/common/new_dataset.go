package common

import (
	"encoding/json"
	"fmt"
	"integration/app/utils"
	"io"
	"net/http"
)

type NewDatasetRequest struct {
	Dataverse    string `json:"dataverse"`
	DataverseKey string `json:"dataverseKey"`
}

type NewDatasetResponse struct {
	PersistentId string `json:"persistentId"`
}

func NewDataset(w http.ResponseWriter, r *http.Request) {
	req := NewDatasetRequest{}
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	pid, err := utils.CreateNewDataset(req.DataverseKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	res := NewDatasetResponse{
		PersistentId: pid,
	}

	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
