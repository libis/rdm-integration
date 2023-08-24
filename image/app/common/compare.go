// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/tree"
	"io"
	"net/http"
	"time"
)

type CompareRequest struct {
	Data         []tree.Node `json:"data"`
	PersistentId string      `json:"persistentId"`
	DataverseKey string      `json:"dataverseKey"`
}

type Key struct {
	Key string `json:"key"`
}

type CachedResponse struct {
	Key          string               `json:"key"`
	Ready        bool                 `json:"ready"`
	Response     core.CompareResponse `json:"res"`
	ErrorMessage string               `json:"err"`
}

var cacheMaxDuration = 5 * time.Minute

func CacheResponse(res CachedResponse) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	b, _ := json.Marshal(res)
	config.GetRedis().Set(ctx, res.Key, string(b), cacheMaxDuration)
}

// this is called after specific compare request (e.g. github compare)
func GetCachedResponse(w http.ResponseWriter, r *http.Request) {
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

	res := CachedResponse{Key: key.Key}
	cached := config.GetRedis().Get(r.Context(), res.Key)
	if cached.Val() != "" {
		json.Unmarshal([]byte(cached.Val()), &res)
		config.GetRedis().Del(r.Context(), res.Key)
		res.Ready = true
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

// this is called when polling for status changes, after specific compare is finished or store is calleed
func Compare(w http.ResponseWriter, r *http.Request) {
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

	errMessage := config.GetRedis().Get(r.Context(), fmt.Sprintf("error %v", req.PersistentId))
	if errMessage != nil && errMessage.Val() != "" {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Job failed: %v", errMessage)))
		return
	}

	//map nodes
	nm := map[string]tree.Node{}
	for _, v := range req.Data {
		nm[v.Id] = v
	}

	//compare and write response
	user := core.GetUserFromHeader(r.Header)
	res := core.Compare(r.Context(), nm, req.PersistentId, req.DataverseKey, user, false)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
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
