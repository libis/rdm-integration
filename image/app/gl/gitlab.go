package gl

import (
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type CompareRequest struct {
	Base         string `json:"base"`
	Token        string `json:"token"`
	Group        string `json:"group"`
	Project      string `json:"project"`
	Hash         string `json:"hash"`
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
}

type GitlabTree struct {
	Entries []GitlabEntry
}

type GitlabEntry struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

func GitlabCompare(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisReady() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := CompareRequest{}
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
	key := fmt.Sprintf("cached compare response (%v): %v", utils.GitHash, req.PersistentId)
	go doGitlabCompare(req, key)
	res := common.Key{Key: key}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func doGitlabCompare(req CompareRequest, key string) {
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

	//query gitlab
	entries := []GitlabEntry{}
	page := 1
	pageEntries, err := getPageEntries(req, page)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}
	for len(pageEntries) > 0 {
		entries = append(entries, pageEntries...)
		page = page + 1
		pageEntries, err = getPageEntries(req, page)
		if err != nil {
			cachedRes.ErrorMessage = err.Error()
			common.CacheResponse(cachedRes)
			return
		}
	}
	tr := GitlabTree{entries}
	utils.MergeNodeMaps(nm, toNodeMap(tr))

	//compare and write response
	res := utils.Compare(nm, req.PersistentId, req.DataverseKey)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	cachedRes.Response = res
	common.CacheResponse(cachedRes)
}

func getPageEntries(req CompareRequest, page int) ([]GitlabEntry, error) {
	res := []GitlabEntry{}
	sep := "/"
	if req.Group == "" {
		sep = ""
	}
	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?recursive=true&ref=main&per_page=100&page=%d", req.Base, url.PathEscape(req.Group+sep+req.Project), page)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("PRIVATE-TOKEN", req.Token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(b, &res)
	return res, err
}

func toNodeMap(tr GitlabTree) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, e := range tr.Entries {
		path := e.Path
		isFile := e.Type == "blob"
		parentId := ""
		ancestors := strings.Split(path, "/")
		fileName := path
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		node := tree.Node{
			Id:   path,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     e.Id,
				RemoteHashType: utils.GitHash,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    0, // file size not known...
						Checksum: tree.Checksum{
							Type:  utils.GitHash,
							Value: e.Id,
						},
					},
				},
			},
		}
		res[path] = node
	}
	return res
}
