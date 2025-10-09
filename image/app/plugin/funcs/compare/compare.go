// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/config"
	"integration/app/core"
	"integration/app/core/oauth"
	"integration/app/logging"
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

// NOTE: Keep the regexes AND the human-readable strings in sync.
// Whenever you change the regex or add an extra rule, update both the compiled pattern
// and the string returned to the client (search for buildAllowed*Description helpers).
// Simplified equivalent of the previous pattern (removed redundant escapes & duplicate backslash)
// Disallowed chars: : < > ; # " / | ? * \
var (
	fileNameR, _                    = regexp.Compile(`^[^:<>;#"/|?*\\]*$`)
	folderNameR, _                  = regexp.Compile(`^[A-Za-z0-9_. /\\-]*$`)
	disallowedFileNameChars         = []string{":", "<", ">", ";", "#", "\"", "/", "|", "?", "*", "\\"}
	disallowedFolderPathPrefixes    = []string{".", "-", "/"}
	allowedFolderPathCharSummary    = "letters, digits, spaces, underscores (_), hyphens (-), dots (.), forward slashes (/), or backslashes (\\)"
	fileNameRuleDescriptionCached   = buildAllowedFileNameDescription()
	folderPathRuleDescriptionCached = buildAllowedFolderPathDescription()
)

func buildAllowedFileNameDescription() string {
	return fmt.Sprintf("Cannot contain any of: %s", strings.Join(disallowedFileNameChars, ", "))
}

func buildAllowedFolderPathDescription() string {
	return fmt.Sprintf("Allowed characters: %s. Must not start with %s.", allowedFolderPathCharSummary, humanizeQuotedList(disallowedFolderPathPrefixes))
}

func humanizeQuotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("\"%s\"", item))
	}
	if len(quoted) == 0 {
		return ""
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + ", or " + quoted[len(quoted)-1]
}

func isFolderPathAllowed(path string) bool {
	if len(path) == 0 {
		return true
	}
	for _, prefix := range disallowedFolderPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return folderNameR.MatchString(path)
}

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
	if !strings.HasSuffix(req.PersistentId, types.NewDataset) {
		err := core.Destination.CheckPermission(ctx, req.DataverseKey, user, req.PersistentId)
		if err != nil {
			cachedRes.ErrorMessage = err.Error()
			common.CacheResponse(cachedRes)
			logging.Logger.Println("no permission")
			return
		}
	}

	//query dataverse
	nm := map[string]tree.Node{}
	var err error
	if !strings.HasSuffix(req.PersistentId, types.NewDataset) {
		nm, err = core.Destination.Query(ctx, req.PersistentId, req.DataverseKey, user)
		if err != nil {
			cachedRes.ErrorMessage = err.Error()
			common.CacheResponse(cachedRes)
			logging.Logger.Println("query failed")
			return
		}
	}

	//query repository
	nmCopy := map[string]tree.Node{}
	for k, v := range nm {
		nmCopy[k] = v
	}
	req.SessionId = req.Token
	req.Token = oauth.GetTokenFromCache(ctx, req.Token, req.SessionId, req.PluginId)
	repoNm, err := plugin.GetPlugin(req.Plugin).Query(ctx, req, nmCopy)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		logging.Logger.Println("plugin query failed")
		return
	}
	rejectedSize := []string{}
	rejectedName := []string{}
	maxFileSize := config.GetMaxFileSize()
	for k, v := range repoNm {
		if maxFileSize > 0 && v.Attributes.RemoteFileSize > maxFileSize {
			delete(repoNm, k)
			rejectedSize = append(rejectedSize, v.Id)
		} else if !fileNameR.MatchString(v.Name) || !isFolderPathAllowed(v.Path) {
			delete(repoNm, k)
			rejectedName = append(rejectedName, v.Id)
		} else if len(strings.TrimSpace(v.Name)) == 0 {
			delete(repoNm, k)
		}
	}
	nm = core.MergeNodeMaps(nm, repoNm)

	//compare and write response
	res := core.Compare(ctx, nm, req.PersistentId, req.DataverseKey, user, true)

	cachedRes.Response = res
	cachedRes.Response.MaxFileSize = maxFileSize
	cachedRes.Response.RejectedSize = rejectedSize
	cachedRes.Response.RejectedName = rejectedName
	cachedRes.Response.AllowedFileNamePattern = fileNameRuleDescriptionCached
	cachedRes.Response.AllowedFolderPathPattern = folderPathRuleDescriptionCached
	common.CacheResponse(cachedRes)
}
