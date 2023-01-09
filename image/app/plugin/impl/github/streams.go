package github

import (
	"bytes"
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	user := streamParams.User
	repo := streamParams.RepoName
	token := streamParams.Token
	if user == "" || repo == "" || token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected user, repo and token, got: %v", streamParams)
	}
	res := map[string]types.Stream{}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	for k, v := range in {
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return nil, fmt.Errorf("streams: sha not found")
		}

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				b2, _, gitErr := client.Git.GetBlobRaw(ctx, user, repo, sha)
				return bytes.NewReader(b2), gitErr
			},
			Close: func() error {
				return nil
			},
		}
	}
	return res, nil
}
