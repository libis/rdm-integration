// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"sort"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	user := ""
	repo := ""
	splitted := strings.Split(params.RepoName, "/")
	if len(splitted) > 1 {
		user = splitted[0]
		repo = strings.Join(splitted[1:], "/")
	}
	token := params.Token
	if user == "" || repo == "" || token == "" {
		return nil, fmt.Errorf("branches: missing parameters: expected user, repo and token")
	}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	defer tc.CloseIdleConnections()
	client := github.NewClient(tc)

	opt := &github.ListOptions{Page: 1, PerPage: 100}
	b, _, err := client.Repositories.ListBranches(ctx, user, repo, opt)
	if err != nil {
		return nil, err
	}
	branches := []*github.Branch{}
	branches = append(branches, b...)
	opt.Page++
	for ; len(b) > 0; opt.Page++ {
		b, _, err = client.Repositories.ListBranches(ctx, user, repo, opt)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b...)
	}

	r, _, err := client.Repositories.Get(ctx, user, repo)
	if err != nil {
		return nil, err
	}
	defaultBranch := r.GetDefaultBranch()
	masterBranch := r.GetMasterBranch()

	sort.Slice(branches, func(i, j int) bool {
		if branches[i].GetName() == masterBranch {
			return true
		}
		if branches[i].GetName() == defaultBranch {
			return true
		}
		return false
	})

	res := []types.SelectItem{}
	for _, v := range branches {
		res = append(res, types.SelectItem{Label: v.GetName(), Value: v.GetName()})
	}
	return res, nil
}
