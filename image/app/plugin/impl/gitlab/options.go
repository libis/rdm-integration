package gitlab

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
	"sort"
)

func Options(params types.OptionsRequest) ([]string, error) {
	base := params.Url
	group := params.User
	project := params.RepoName
	token := params.Token
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
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
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
