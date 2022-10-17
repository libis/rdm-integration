package utils

import (
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
		var readStream io.ReadCloser
		res[k] = stream{
			Open: func() (io.Reader, error) {
				return getBlob(ctx, user, repo, sha, client)
			},
			Close: func() error {
				var closeErr error
				if readStream != nil {
					closeErr = readStream.Close()
				}
				return closeErr
			},
		}
	}
	return res, nil
}

func getBlob(ctx context.Context, owner, repo, sha string, client *github.Client) (io.ReadCloser, error) {
	u := fmt.Sprintf("repos/%v/%v/git/blobs/%v", owner, repo, sha)
	req, err := client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	req = req.WithContext(ctx)

	reader, writer := io.Pipe()
	_, err = client.Do(ctx, req, writer)
	return reader, err
}
