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
	url := base + "/api/v4/projects/" + url.PathEscape(project) + "/repository/branches"
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("getting branches failed: %s", string(b))
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
	res := []types.SelectItem{}
	for _, v := range branches {
		res = append(res, types.SelectItem{Label: v.Name, Value: v.Name})
	}
	return res, nil
}
