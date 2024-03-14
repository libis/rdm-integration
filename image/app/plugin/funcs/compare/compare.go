// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var fileNameR, _ = regexp.Compile(`^[^:<>;#"\/\*\|\?\\]*$`)
var folderNameR, _ = regexp.Compile(`^[a-zA-Z0-9_\.\/\- \\]*$`)

func Compare(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	user := core.GetUserFromHeader(r.Header)
	//process request
	req := types.CompareRequest{}
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
	key := uuid.New().String()
	go doCompare(req, key, user)
	res := common.Key{Key: key}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func doCompare(req types.CompareRequest, key, user string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	cachedRes := common.CachedResponse{
		Key: key,
	}
	//check permission
	err := core.Destination.CheckPermission(ctx, req.DataverseKey, user, req.PersistentId)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	//query dataverse
	nm, err := core.Destination.Query(ctx, req.PersistentId, req.DataverseKey, user)
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
	req.Token = core.GetTokenFromCache(ctx, req.Token, req.Token, req.PluginId)
	repoNm, err := plugin.GetPlugin(req.Plugin).Query(ctx, req, nmCopy)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}
	rejected := []string{}
	maxFileSize := config.GetMaxFileSize()
	for k, v := range repoNm {
		if maxFileSize > 0 && v.Attributes.RemoteFilesize > maxFileSize {
			delete(repoNm, k)
			rejected = append(rejected, v.Id)
		} else if !fileNameR.MatchString(v.Name) || !folderNameR.MatchString(v.Path) {
			delete(repoNm, k)
			rejected = append(rejected, v.Id)
		} else if len(strings.TrimSpace(v.Name)) == 0 {
			delete(repoNm, k)
		}
	}
	nm = core.MergeNodeMaps(nm, repoNm)

	//compare and write response
	res := core.Compare(ctx, nm, req.PersistentId, req.DataverseKey, user, true)

	//copy metadata if the source is a Dataverse installation and destination is a newly created dataset
	if req.Plugin == "dataverse" && req.NewlyCreated {
		err = copyMetaData(req, user)
		if err != nil {
			cachedRes.ErrorMessage = err.Error()
			common.CacheResponse(cachedRes)
			return
		}
	}

	cachedRes.Response = res
	cachedRes.Response.MaxFileSize = maxFileSize
	cachedRes.Response.Rejected = rejected
	common.CacheResponse(cachedRes)
}
