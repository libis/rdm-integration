// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

type NewDatasetRequest struct {
	Collection   string         `json:"collection"`
	DataverseKey string         `json:"dataverseKey"`
	Metadata     types.Metadata `json:"metadata"`
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
		w.Write([]byte("500 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	user := core.GetUserFromHeader(r.Header)
	pid, err := core.Destination.CreateNewRepo(r.Context(), req.Collection, req.DataverseKey, user, req.Metadata)
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
