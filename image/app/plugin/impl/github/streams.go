// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	user := ""
	repo := ""
	splitted := strings.Split(streamParams.RepoName, "/")
	if len(splitted) > 1 {
		user = splitted[0]
		repo = strings.Join(splitted[1:], "/")
	}
	token := streamParams.Token
	if user == "" || repo == "" || token == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: expected user, repo and token")
	}
	res := map[string]types.Stream{}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	defer tc.CloseIdleConnections()

	client := github.NewClient(tc)
	for k, v := range in {
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return types.StreamsType{}, fmt.Errorf("streams: sha not found")
		}
		var gitErr error
		var err error
		var reader io.ReadCloser

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				reader, err = GetBlobRaw(client, ctx, user, repo, sha, gitErr)
				return reader, err
			},
			Close: func() error {
				reader.Close()
				return gitErr
			},
		}
	}
	return types.StreamsType{Streams: res, Cleanup: nil}, nil
}

func GetBlobRaw(client *github.Client, ctx context.Context, owner, repo, sha string, err error) (io.ReadCloser, error) {
	u := fmt.Sprintf("repos/%v/%v/git/blobs/%v", owner, repo, sha)
	req, reqErr := client.NewRequest("GET", u, nil)
	if reqErr != nil {
		return nil, reqErr
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	pr, pw := io.Pipe()
	go func() {
		_, err = client.Do(ctx, req, pw)
		pw.Close()
	}()
	return pr, nil
}
