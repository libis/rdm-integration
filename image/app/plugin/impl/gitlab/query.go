package gitlab

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"net/url"
	"strings"
)

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

func Query(req types.CompareRequest) (map[string]tree.Node, error) {
	entries := []GitlabEntry{}
	page := 1
	pageEntries, err := getPageEntries(req, page)
	if err != nil {
		return nil, err
	}
	for len(pageEntries) > 0 {
		entries = append(entries, pageEntries...)
		page = page + 1
		pageEntries, err = getPageEntries(req, page)
		if err != nil {
			return nil, err
		}
	}
	tr := GitlabTree{entries}
	return toNodeMap(tr), nil
}

func getPageEntries(req types.CompareRequest, page int) ([]GitlabEntry, error) {
	res := []GitlabEntry{}
	sep := "/"
	if req.User == "" {
		sep = ""
	}
	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?recursive=true&ref=%s&per_page=100&page=%d", req.Url, url.PathEscape(req.User+sep+req.RepoName), req.Option, page)
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
				RemoteHashType: types.GitHash,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    0, // file size not known...
						Checksum: tree.Checksum{
							Type:  types.GitHash,
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
