// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/core/oauth"
	"integration/app/plugin/impl/globus"
	"integration/app/tree"
	"io"
	"net/http"
)

func Download(w http.ResponseWriter, r *http.Request) {
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
		if v.Attributes.IsFile {
			selected[v.Id] = v
		}
	}

	user := core.GetUserFromHeader(r.Header)
	if req.StreamParams.User == "" {
		req.StreamParams.User = user
	}
	req.StreamParams.SessionId = core.GetSessionId(r.Header)
	req.StreamParams.PersistentId = req.PersistentId
	req.StreamParams.Token = oauth.GetTokenFromCache(r.Context(), req.StreamParams.Token, req.StreamParams.SessionId, "globus")
	res := ""
	res, err = globus.Download(r.Context(), req.StreamParams, selected)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(fmt.Appendf(nil, "500 - %v", err))
		return
	}
	type downloadResponse struct {
		TaskId     string `json:"taskId"`
		MonitorUrl string `json:"monitorUrl,omitempty"`
	}
	response := downloadResponse{TaskId: res}
	if res != "" {
		response.MonitorUrl = globus.TaskActivityURL(res)
	}
	resBytes, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(fmt.Appendf(nil, "500 - %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resBytes)
}

func GlobusTransferStatus(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	taskId := r.URL.Query().Get("taskId")
	if taskId == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - taskId is required"))
		return
	}
	sessionId := core.GetSessionId(r.Header)
	token := oauth.GetTokenFromCache(r.Context(), "", sessionId, "globus")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("401 - globus session expired"))
		return
	}
	resBytes, err := globus.GetTaskStatus(r.Context(), token, taskId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(fmt.Appendf(nil, "500 - %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resBytes)
}
