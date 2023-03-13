// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
)

type StoreResult struct {
	Status    string `json:"status"`
	DatsetUrl string `json:"datasetUrl"`
}

type StoreRequest struct {
	Plugin            string             `json:"plugin"`
	StreamParams      types.StreamParams `json:"streamParams"`
	PersistentId      string             `json:"persistentId"`
	DataverseKey      string             `json:"dataverseKey"`
	SelectedNodes     []tree.Node        `json:"selectedNodes"`
	SendEmailOnSucces bool               `json:"sendEmailOnSucces"`
}

func Store(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	req := StoreRequest{}
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

	selected := map[string]tree.Node{}
	for _, v := range req.SelectedNodes {
		selected[v.Id] = v
	}

	user := core.GetUserFromHeader(r.Header)
	sessionId := core.GetShibSessionFromHeader(r.Header)
	err = core.AddJob(r.Context(), core.Job{
		DataverseKey:      req.DataverseKey,
		User:              user,
		SessionId:         sessionId,
		PersistentId:      req.PersistentId,
		WritableNodes:     selected,
		Plugin:            req.Plugin,
		StreamParams:      req.StreamParams,
		SendEmailOnSucces: req.SendEmailOnSucces,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	res := StoreResult{
		Status:    "OK",
		DatsetUrl: core.Destination.GetRepoUrl(req.PersistentId, true),
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
