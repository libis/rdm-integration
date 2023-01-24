package common

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
)

type StoreResult struct {
	Status    string `json:"status"`
	DatsetUrl string `json:"datasetUrl"`
}

type StoreRequest struct {
	StreamType    string             `json:"streamType"`
	StreamParams  types.StreamParams `json:"streamParams"`
	PersistentId  string             `json:"persistentId"`
	DataverseKey  string             `json:"dataverseKey"`
	SelectedNodes []tree.Node        `json:"selectedNodes"`
}

func Store(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisReady() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	req := StoreRequest{}
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

	selected := map[string]tree.Node{}
	for _, v := range req.SelectedNodes {
		selected[v.Id] = v
	}

	err = utils.AddJob(utils.Job{
		DataverseKey:  req.DataverseKey,
		PersistentId:  req.PersistentId,
		WritableNodes: selected,
		StreamType:    req.StreamType,
		StreamParams:  req.StreamParams,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	res := StoreResult{
		Status:    "OK",
		DatsetUrl: utils.GetDatasetUrl(req.PersistentId, true),
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
