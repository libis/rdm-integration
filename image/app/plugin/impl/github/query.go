// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func Query(req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: req.Token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	tr, _, err := client.Git.GetTree(ctx, req.User, req.RepoName, req.Option, true)
	if err != nil {
		return nil, err
	}
	return toNodeMap(tr), nil
}

func toNodeMap(tr *github.Tree) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, e := range tr.Entries {
		isFile := e.GetType() == "blob"
		if !isFile {
			continue
		}

		id := e.GetPath()
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
				URL:            e.GetURL(),
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     e.GetSHA(),
				RemoteHashType: types.GitHash,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    e.GetSize(),
						Checksum: tree.Checksum{
							Type:  types.GitHash,
							Value: e.GetSHA(),
						},
					},
				},
			},
		}
		res[id] = node
	}
	return res
}
