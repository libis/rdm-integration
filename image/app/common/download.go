// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/core/oauth"
	"integration/app/logging"
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
	// For OAuth plugins, req.StreamParams.Token contains the OAuth session ID
	// For logged-in users with SSO, core.GetSessionId returns Shibboleth session, not OAuth session
	// So use req.StreamParams.Token as the lookup key for OAuth tokens
	oauthSessionId := req.StreamParams.Token
	req.StreamParams.SessionId = req.StreamParams.Token
	req.StreamParams.PersistentId = req.PersistentId
	logging.Logger.Printf("Download: user=%s, oauthSessionId=%s, dvToken=%v", user, oauthSessionId, req.DataverseKey != "")
	req.StreamParams.Token = oauth.GetTokenFromCache(r.Context(), req.StreamParams.Token, oauthSessionId, "globus")
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
	// For OAuth plugins, try to get oauthSessionId from query params first (guest/OAuth flow)
	// Fall back to session from headers (SSO flow)
	oauthSessionId := r.URL.Query().Get("oauthSessionId")
	if oauthSessionId == "" {
		oauthSessionId = core.GetSessionId(r.Header)
	}
	token := oauth.GetTokenFromCache(r.Context(), "", oauthSessionId, "globus")
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
