package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

type stream struct {
	Open  func() (io.Reader, error)
	Close func() error
}

func deserialize(ctx context.Context, nodeMap map[string]tree.Node, streamType string, streamParams map[string]string) (map[string]stream, error) {
	switch streamType {
	case "github":
		return toGithubStreams(ctx, nodeMap, streamParams)
	case "gitlab":
		return toGitlabStreams(ctx, nodeMap, streamParams)
	default:
		return nil, fmt.Errorf("unknown stream type: %s", streamType)
	}
}

func toGithubStreams(ctx context.Context, in map[string]tree.Node, streamParams map[string]string) (map[string]stream, error) {
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
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return nil, fmt.Errorf("streams: sha not found")
		}

		res[k] = stream{
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

func toGitlabStreams(ctx context.Context, in map[string]tree.Node, streamParams map[string]string) (map[string]stream, error) {
	base := streamParams["base"]
	group := streamParams["group"]
	project := streamParams["project"]
	token := streamParams["token"]
	if project == "" || token == "" || base == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected base, group (optional), project and token, got: %v", streamParams)
	}
	res := map[string]stream{}

	for k, v := range in {
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return nil, fmt.Errorf("streams: sha not found")
		}
		sep := "/"
		if group == "" {
			sep = ""
		}
		url := base + "/api/v4/projects/" + url.PathEscape(group+sep+project) + "/repository/blobs/" + sha + "/raw"
		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Add("PRIVATE-TOKEN", token)
		var r *http.Response

		res[k] = stream{
			Open: func() (io.Reader, error) {
				fmt.Println(url)
				r, err = http.DefaultClient.Do(request)
				if err != nil {
					return nil, err
				}
				if r.StatusCode != 200 {
					b, _ := io.ReadAll(r.Body)
					return nil, fmt.Errorf("getting file failed: %s", string(b))
				}
				return r.Body, nil
			},
			Close: func() error {
				return r.Body.Close()
			},
		}
	}
	return res, nil
}

func GithubBranches(params map[string]string) ([]string, error) {
	user := params["user"]
	repo := params["repo"]
	token := params["token"]
	if user == "" || repo == "" || token == "" {
		return nil, fmt.Errorf("branches: missing parameters: expected user, repo and token, got: %v", params)
	}
	return []string{"main", "master"}, nil
}

func GitlabBranches(params map[string]string) ([]string, error) {
	base := params["base"]
	group := params["group"]
	project := params["project"]
	token := params["token"]
	if project == "" || token == "" || base == "" {
		return nil, fmt.Errorf("branches: missing parameters: expected base, group (optional), project and token, got: %v", params)
	}
	sep := "/"
	if group == "" {
		sep = ""
	}
	url := base + "/api/v4/projects/" + url.PathEscape(group+sep+project) + "/repository/branches"
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("PRIVATE-TOKEN", token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("getting file failed: %s", string(b))
	}
	type Commit struct {
		CommittedDate string `json:"committed_date"`
	}
	type Branch struct {
		Name    string `json:"name"`
		Default bool   `json:"default"`
		Commit  Commit `json:"commit"`
	}
	branches := []Branch{}
	err = json.Unmarshal(b, &branches)
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Default {
			return true
		}
		return branches[i].Commit.CommittedDate > branches[j].Commit.CommittedDate
	})
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, v := range branches {
		res = append(res, v.Name)
	}
	return res, nil
}
