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

	"github.com/google/uuid"
)

// this is called when polling for status changes, after specific compare is finished or store is called
func Compute(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := core.ComputeRequest{}
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
	access, err := checkAccess(AccessRequest{
		PersistentId: req.PersistentId,
		DataverseKey: req.DataverseKey,
		Queue:        req.Queue,
	}, r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - access check failed"))
		return
	}
	if !access.Access {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - access denied"))
		return
	}

	key := uuid.New().String()
	res := Key{Key: key}
	core.CacheComputeResponse(core.CachedComputeResponse{
		Key:          key,
		Ready:        false,
		ConsoleOut:   "running...",
		ErrorMessage: "",
	})
	err = core.AddJob(r.Context(), core.Job{
		DataverseKey:       req.DataverseKey,
		User:               core.GetUserFromHeader(r.Header),
		SessionId:          core.GetSessionId(r.Header),
		PersistentId:       req.PersistentId,
		WritableNodes:      map[string]tree.Node{req.Executable: {}},
		Plugin:             "compute",
		SendEmailOnSuccess: req.SenSendEmailOnSuccess,
		Key:                key,
		Queue:              req.Queue,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - failed to add job: " + err.Error()))
		return
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

// this is called after specific compare request (e.g. github compare)
func GetCachedComputeResponse(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	key := Key{}
	err = json.Unmarshal(b, &key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	res := core.CachedComputeResponse{Key: key.Key}
	cached := config.GetRedis().Get(r.Context(), res.Key)
	if cached.Val() != "" {
		json.Unmarshal([]byte(cached.Val()), &res)
		config.GetRedis().Del(r.Context(), res.Key)
	}
	if res.ErrorMessage != "" {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", res.ErrorMessage)))
		return
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
