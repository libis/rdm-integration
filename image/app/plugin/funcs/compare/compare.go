// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package compare

import (
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
)

func Compare(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisReady() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := types.CompareRequest{}
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
	key := fmt.Sprintf("cached compare response (%v): %v", types.GitHash, req.PersistentId)
	go doCompare(req, key)
	res := common.Key{Key: key}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func doCompare(req types.CompareRequest, key string) {
	cachedRes := common.CachedResponse{
		Key: key,
	}
	//check permission
	err := utils.CheckPermission(req.DataverseKey, req.PersistentId)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	//query dataverse
	nm, err := utils.GetNodeMap(req.PersistentId, req.DataverseKey)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	//query repository
	nmCopy := map[string]tree.Node{}
	for k, v := range nm {
		nmCopy[k] = v
	}
	repoNm, err := plugin.GetPlugin(req.RepoType).Query(req, nmCopy)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}
	nm = utils.MergeNodeMaps(nm, repoNm)

	//compare and write response
	res := utils.Compare(nm, req.PersistentId, req.DataverseKey, true)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	cachedRes.Response = res
	common.CacheResponse(cachedRes)
}
