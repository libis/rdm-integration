// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package gitlab

import (
	"context"
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

func Query(ctx context.Context, req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	entries := []GitlabEntry{}
	page := 1
	pageEntries, err := getPageEntries(ctx, req, page)
	if err != nil {
		return nil, err
	}
	for len(pageEntries) > 0 {
		entries = append(entries, pageEntries...)
		page = page + 1
		pageEntries, err = getPageEntries(ctx, req, page)
		if err != nil {
			return nil, err
		}
	}
	tr := GitlabTree{entries}
	return toNodeMap(tr), nil
}

func getPageEntries(ctx context.Context, req types.CompareRequest, page int) ([]GitlabEntry, error) {
	res := []GitlabEntry{}
	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?recursive=true&ref=%s&per_page=100&page=%d", req.Url, url.PathEscape(req.RepoName), req.Option, page)
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Authorization", "Bearer "+req.Token)
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
		isFile := e.Type == "blob"
		if !isFile {
			continue
		}

		id := e.Path
		parentId := ""
		ancestors := strings.Split(id, "/")
		fileName := id
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		node := tree.Node{
			Id:   id,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				IsFile:         isFile,
				RemoteHash:     e.Id,
				RemoteHashType: types.GitHash,
			},
		}
		res[id] = node
	}
	return res
}
