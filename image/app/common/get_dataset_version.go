// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"io"
	"net/http"
)

type DatasetVersionRequest struct {
	DatasetDbId   string `json:"datasetDbId"`
	DataverseKey string `json:"dataverseKey"`
}

type DatasetVersionResponse struct {
	PersistentId string `json:"persistentId"`
}

func GetDatasetVersion(w http.ResponseWriter, r *http.Request) {
	req := DatasetVersionRequest{}
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
	pid, err := core.Destination.GetDatasetVersion(r.Context(), req.DatasetDbId, req.DataverseKey, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	res := DatasetVersionResponse{
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
