// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

type SearchResults struct {
	Items []Item `json:"items"`
}

type Item struct {
	FullName string `json:"full_name"`
}

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	token := params.Token
	if token == "" {
		return nil, fmt.Errorf("not authorized")
	}
	url := "https://api.github.com/search/repositories?q=" + params.RepoName
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub search request could not be created")
	}
	request.Header.Add("Accept", "application/vnd.github+json")
	request.Header.Add("Authorization", "Bearer "+token)
	request.Header.Add("X-GitHub-Api-Version", "2022-11-28")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("GitHub search request failed")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub search failed with status %d", r.StatusCode)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("GitHub search response could not be read")
	}
	results := SearchResults{}
	err = json.Unmarshal(b, &results)
	if err != nil {
		return nil, fmt.Errorf("GitHub search response could not be parsed")
	}

	res := []types.SelectItem{}
	for _, v := range results.Items {
		res = append(res, types.SelectItem{Label: v.FullName, Value: v.FullName})
	}
	return res, nil
}
