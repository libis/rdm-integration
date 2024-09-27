// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"
)

func Query(ctx context.Context, req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	folder := req.Option
	if folder == "" {
		folder = "/"
	}
	entries, err := listItems(ctx, folder, req.Url+"/operation/endpoint/"+req.RepoName+"/ls", req.Token, true)
	if err != nil {
		return nil, err
	}
	return toNodeMap(folder, entries)
}

func toNodeMap(folder string, entries []Entry) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(e.Id, folder), "/"), "/")
		path := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(e.Path, folder), "/"), "/")
		node := tree.Node{
			Id:   id,
			Name: e.Name,
			Path: path,
			Attributes: tree.Attributes{
				URL:            e.URL,
				IsFile:         !e.IsDir,
				RemoteHash:     e.Hash,
				RemoteHashType: e.HashType,
				RemoteFilesize: e.Size,
			},
		}
		res[id] = node
	}
	return res, nil
}
