// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
)

func Query(ctx context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	url := fmt.Sprintf("%s/api/", req.Url)
	nodes, err := getNodes(ctx, url, req.RepoName, req.Token)
	if err != nil {
		return nil, err
	}
	return toNodeMap(nodes, nm, url, req.Token)
}

func toNodeMap(nodes []Node, nm map[string]tree.Node, url, token string) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	for _, node := range nodes {
		if node.IsDir {
			continue
		}
		hashType, hash, err := hash(node, nm, token)
		if err != nil {
			return nil, err
		}

		node := tree.Node{
			Id:   node.Id,
			Name: node.Name,
			Path: node.Path,
			Attributes: tree.Attributes{
				URL:            node.URL,
				ParentId:       node.Path,
				IsFile:         !node.IsDir,
				RemoteHash:     hash,
				RemoteHashType: hashType,
				Metadata: tree.Metadata{
					Label:          node.Name,
					DirectoryLabel: node.Path,
					DataFile: tree.DataFile{
						Filename:    node.Name,
						ContentType: "application/octet-stream",
						Filesize:    node.Size,
						Checksum: tree.Checksum{
							Type:  hashType,
							Value: hash,
						},
					},
				},
			},
		}
		res[node.Id] = node
	}
	return res, nil
}

func hash(node Node, nm map[string]tree.Node, token string) (string, string, error) {
	if node.HashType != "" {
		return node.HashType, node.Hash, nil
	}
	if _, ok := nm[node.Id]; !ok {
		return types.Md5, types.NotNeeded, nil
	}
	req, _ := http.NewRequest("GET", node.URL, nil)
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	hasher := md5.New()
	_, err = io.Copy(hasher, resp.Body)
	return types.Md5, fmt.Sprintf("%x", hasher.Sum(nil)), err
}
