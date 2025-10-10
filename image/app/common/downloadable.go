// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/tree"
	"io"
	"net/http"
)

func GetDownloadableFiles(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := CompareRequest{}
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

	//get files and write response
	user := core.GetUserFromHeader(r.Header)
	nm, err := core.Destination.Query(r.Context(), req.PersistentId, req.DataverseKey, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - getting files failed"))
		return
	}
	data := []tree.Node{}
	for _, node := range nm {
		if node.Attributes.IsFile {
			data = append(data, node)
		}
	}
	res := core.CompareResponse{
		Id:     req.PersistentId,
		Status: core.Finished,
		Data:   data,
		Url:    core.Destination.GetRepoUrl(req.PersistentId, false),
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
