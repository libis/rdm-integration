package github

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func Metadata(ctx context.Context, streamParams types.StreamParams) (types.MetadataStruct, error) {
	user := ""
	repo := ""
	splitted := strings.Split(streamParams.RepoName, "/")
	if len(splitted) > 1 {
		user = splitted[0]
		repo = strings.Join(splitted[1:], "/")
	}
	token := streamParams.Token
	if user == "" || repo == "" || token == "" {
		return types.MetadataStruct{}, fmt.Errorf("metadata: missing parameters: expected user, repo and token")
	}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	defer tc.CloseIdleConnections()

	client := github.NewClient(tc)
	res, _, err := client.Repositories.Get(ctx, user, repo)
	if err != nil {
		return types.MetadataStruct{}, err
	}

	var description []string
	if res.Description != nil && *res.Description != "" {
		description = []string{*res.Description}
	}

	return types.MetadataStruct{
		Title:            *res.Name,
		AlternativeTitle: []string{*res.FullName},
		AlternativeURL:   []string{*res.HTMLURL},
		OtherId:          []types.OtherId{{OtherIdAgency: "Git", OtherIdValue: *res.GitURL}, {OtherIdAgency: "GitHub", OtherIdValue: "https://api.github.com/repositories/" + fmt.Sprint(*res.ID)}},
		DsDescription:    description,
		Keyword:          res.Topics,
	}, nil
}
