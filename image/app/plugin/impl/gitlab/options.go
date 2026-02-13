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
	"sort"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	base := params.Url
	project := params.RepoName
	token := params.Token
	if project == "" || token == "" || base == "" {
		return nil, fmt.Errorf("branches: missing parameters: expected base, group (optional), project and token")
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
	const perPage = 100
	for page := 1; ; page++ {
		pageURL := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/branches?per_page=%d&page=%d",
			base,
			url.PathEscape(project),
			perPage,
			page,
		)
		request, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Add("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return nil, err
		}
		if r.StatusCode != 200 {
			return nil, fmt.Errorf("getting branches failed: %s", string(b))
		}
		pageBranches := []Branch{}
		err = json.Unmarshal(b, &pageBranches)
		if err != nil {
			return nil, err
		}
		if len(pageBranches) == 0 {
			break
		}
		branches = append(branches, pageBranches...)
	}
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Default {
			return true
		}
		return branches[i].Commit.CommittedDate > branches[j].Commit.CommittedDate
	})
	res := []types.SelectItem{}
	for _, v := range branches {
		res = append(res, types.SelectItem{Label: v.Name, Value: v.Name})
	}
	return res, nil
}
