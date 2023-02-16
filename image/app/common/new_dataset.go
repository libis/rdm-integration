// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"io"
	"net/http"
)

type NewDatasetRequest struct {
	Collection   string `json:"collection"`
	DataverseKey string `json:"dataverseKey"`
}

type NewDatasetResponse struct {
	PersistentId string `json:"persistentId"`
}

func NewDataset(w http.ResponseWriter, r *http.Request) {
	req := NewDatasetRequest{}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
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

	user := core.GetUserFromHeader(r.Header)
	pid, err := core.Destination.CreateNewRepo(r.Context(), req.Collection, req.DataverseKey, user)
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
