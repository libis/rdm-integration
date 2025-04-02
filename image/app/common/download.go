// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
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
	res := ""
	res, err = globus.Download(r.Context(), req.StreamParams, selected)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(fmt.Appendf(nil, "500 - %v", err))
		return
	}
	w.Write([]byte(res))
}
