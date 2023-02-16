// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
)

func Query(ctx context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	files, err := getFiles(ctx, req.Url, req.RepoName, req.Token)
	if err != nil {
		return nil, err
	}
	return toNodeMap(files, nm, req.Url, req.Token)
}

func toNodeMap(files []File, nm map[string]tree.Node, url, token string) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	for _, file := range files {
		if file.IsDir {
			continue
		}

		node := tree.Node{
			Id:   file.Id,
			Name: file.Name,
			Path: file.Path,
			Attributes: tree.Attributes{
				URL:            file.URL,
				IsFile:         !file.IsDir,
				RemoteHash:     file.Hash,
				RemoteHashType: file.HashType,
				RemoteFilesize: file.Size,
			},
		}
		res[node.Id] = node
	}
	return res, nil
}
