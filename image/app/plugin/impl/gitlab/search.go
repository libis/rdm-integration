// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

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

type Item struct {
	PathWithNamespace string `json:"path_with_namespace"`
}

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	token := params.Token
	if token == "" {
		return nil, fmt.Errorf("not authorized")
	}
	const maxSearchResults = 10
	searchURL := fmt.Sprintf(
		"%s/api/v4/search?scope=projects&search=%s&per_page=%d&page=1",
		params.Url,
		url.QueryEscape(params.RepoName),
		maxSearchResults,
	)
	request, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitLab search request could not be created")
	}
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("GitLab search request failed")
	}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("GitLab search response could not be read")
	}
	if r.StatusCode != 200 && r.StatusCode != 404 {
		return nil, fmt.Errorf("GitLab search failed with status %d", r.StatusCode)
	}
	results := []Item{}
	if r.StatusCode == 200 {
		if err := json.Unmarshal(b, &results); err != nil {
			return nil, fmt.Errorf("GitLab search response could not be parsed")
		}
	}
	res := []types.SelectItem{}
	for _, v := range results {
		res = append(res, types.SelectItem{Label: v.PathWithNamespace, Value: v.PathWithNamespace})
	}
	return res, nil
}
