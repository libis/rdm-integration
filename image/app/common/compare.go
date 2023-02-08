// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"integration/app/utils"
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
	Key          string                `json:"key"`
	Ready        bool                  `json:"ready"`
	Response     utils.CompareResponse `json:"res"`
	ErrorMessage string                `json:"err"`
}

var cacheMaxDuration = time.Minute

func CacheResponse(res CachedResponse) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, _ := json.Marshal(res)
	utils.GetRedis().Set(ctx, res.Key, string(b), cacheMaxDuration)
}

// this is called after specific compare request (e.g. github compare)
func GetCachedResponse(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	key := Key{}
	err = json.Unmarshal(b, &key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	res := CachedResponse{Key: key.Key}
	cached := utils.GetRedis().Get(r.Context(), res.Key)
	if cached.Val() != "" {
		json.Unmarshal([]byte(cached.Val()), &res)
		utils.GetRedis().Del(r.Context(), res.Key)
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
	if !utils.RedisReady(r.Context()) {
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
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	//map nodes
	nm := map[string]tree.Node{}
	for _, v := range req.Data {
		nm[v.Id] = v
	}

	//compare and write response
	user := utils.GetUserFromHeader(r.Header)
	res := utils.Compare(r.Context(), nm, req.PersistentId, req.DataverseKey, user, false)
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
