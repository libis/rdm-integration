// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin/impl/globus"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
)

type StoreResult struct {
	Status                   string `json:"status"`
	DatasetUrl               string `json:"datasetUrl"`
	GlobusTransferTaskId     string `json:"globusTransferTaskId"`
	GlobusTransferMonitorUrl string `json:"globusTransferMonitorUrl"`
}

type StoreRequest struct {
	Plugin             string             `json:"plugin"`
	StreamParams       types.StreamParams `json:"streamParams"`
	PersistentId       string             `json:"persistentId"`
	DataverseKey       string             `json:"dataverseKey"`
	SelectedNodes      []tree.Node        `json:"selectedNodes"`
	SendEmailOnSuccess bool               `json:"sendEmailOnSuccess"`
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
		w.Write([]byte("500 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	selected := map[string]tree.Node{}
	for _, v := range req.SelectedNodes {
		selected[v.Id] = v
	}

	user := core.GetUserFromHeader(r.Header)
	if req.StreamParams.User == "" {
		req.StreamParams.User = user
	}
	job := core.Job{
		DataverseKey:       req.DataverseKey,
		User:               user,
		SessionId:          req.StreamParams.Token,
		PersistentId:       req.PersistentId,
		WritableNodes:      selected,
		Plugin:             req.Plugin,
		StreamParams:       req.StreamParams,
		SendEmailOnSuccess: req.SendEmailOnSuccess,
	}
	if req.Plugin == "globus" {
		job, err = core.DoWork(job)
	} else {
		err = core.AddJob(r.Context(), job)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	monitorUrl := ""
	if job.GlobusTaskId != "" {
		monitorUrl = globus.TaskActivityURL(job.GlobusTaskId)
	}

	res := StoreResult{
		Status:                   "OK",
		DatasetUrl:               core.Destination.GetRepoUrl(req.PersistentId, true),
		GlobusTransferTaskId:     job.GlobusTaskId,
		GlobusTransferMonitorUrl: monitorUrl,
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
