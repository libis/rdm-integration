package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
)

type GitlabRes struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	NameWithNamespace string   `json:"name_with_namespace"`
	Description       string   `json:"description"`
	Topics            []string `json:"topics"`
	WebUrl            string   `json:"web_url"`
}

func Metadata(ctx context.Context, streamParams types.StreamParams) (types.MetadataStruct, error) {
	base := streamParams.Url
	project := streamParams.RepoName
	token := streamParams.Token
	if project == "" || token == "" || base == "" {
		return types.MetadataStruct{}, fmt.Errorf("metadata: missing parameters: expected base, group (optional), project and token")
	}
	res := GitlabRes{}
	url := fmt.Sprintf("%s/api/v4/projects/%s", streamParams.Url, url.PathEscape(streamParams.RepoName))
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	request.Header.Add("Authorization", "Bearer "+streamParams.Token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	return types.MetadataStruct{
		Title:            res.Name,
		AlternativeTitle: []string{res.NameWithNamespace},
		AlternativeURL:   []string{res.WebUrl, fmt.Sprintf("%s/projects/%v", streamParams.Url, res.ID)},
		OtherId:          []types.OtherId{{OtherIdAgency: "Git", OtherIdValue: fmt.Sprintf("urn:git:%s:%s:branch:%s", streamParams.Url, streamParams.RepoName, streamParams.Option)}, {OtherIdAgency: "GitLab", OtherIdValue: fmt.Sprintf("urn:gitlab:%s:project:%v:branch:%s", streamParams.Url, res.ID, streamParams.Option)}},
		DsDescription:    []string{res.Description},
		Keyword:          res.Topics,
	}, nil
}
