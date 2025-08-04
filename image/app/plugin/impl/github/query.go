// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
	"net/http"
	"strings"
	"sync"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// Connection pool for HTTP clients to improve performance
var (
	clientPool = sync.Pool{
		New: func() interface{} {
			return &http.Client{}
		},
	}
)

func Query(ctx context.Context, req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	// Get HTTP client from pool
	httpClient := clientPool.Get().(*http.Client)
	defer clientPool.Put(httpClient)

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: req.Token},
	)
	tc := oauth2.NewClient(ctx, ts)
	// Override the HTTP client with our pooled one to reuse connections
	tc.Transport = &oauth2.Transport{
		Source: ts,
		Base:   httpClient.Transport,
	}
	defer tc.CloseIdleConnections()

	client := github.NewClient(tc)
	user := ""
	repo := ""
	splitted := strings.Split(req.RepoName, "/")
	if len(splitted) > 1 {
		user = splitted[0]
		repo = strings.Join(splitted[1:], "/")
	}
	tr, _, err := client.Git.GetTree(ctx, user, repo, req.Option, true)
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
				IsFile:         isFile,
				RemoteHash:     e.GetSHA(),
				RemoteHashType: types.GitHash,
				RemoteFileSize: int64(e.GetSize()),
			},
		}
		res[id] = node
	}
	return res
}
