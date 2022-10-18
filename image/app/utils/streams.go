package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

type stream struct {
	Open  func() (io.Reader, error)
	Close func() error
}

func deserialize(ctx context.Context, streamType string, streams map[string]map[string]interface{}, streamParams map[string]string) (map[string]stream, error) {
	switch streamType {
	case "github":
		return toGithubStreams(ctx, streams, streamParams)
	default:
		return nil, fmt.Errorf("unknown stream type: %s", streamType)
	}
}

func toGithubStreams(ctx context.Context, in map[string]map[string]interface{}, streamParams map[string]string) (map[string]stream, error) {
	user := streamParams["user"]
	repo := streamParams["repo"]
	token := streamParams["token"]
	if user == "" || repo == "" || token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected user, repo and token, got: %v", streamParams)
	}
	res := map[string]stream{}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	for k, v := range in {
		sha, ok := v["sha"].(string)
		if !ok || sha == "" {
			return nil, fmt.Errorf("streams: sha not found")
		}
		var readStream io.Reader
		var gitErr error
		res[k] = stream{
			Open: func() (io.Reader, error) {
				var b2 []byte
				b2, _, gitErr = client.Git.GetBlobRaw(ctx, user, repo, sha)
				readStream = bytes.NewReader(b2)
				return readStream, gitErr
			},
			Close: func() error {
				return nil
			},
		}
	}
	return res, nil
}
