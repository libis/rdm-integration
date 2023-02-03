// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"strings"
)

type SearchResults struct {
	Items []Item `json:"items"`
}

type Item struct {
	FullName string `json:"full_name"`
}

func Search(params types.OptionsRequest) ([]string, error) {
	token := params.Token
	if token == "" {
		return nil, fmt.Errorf("not authorized")
	}
	searchTerm := ""
	splitted := strings.Split(params.RepoName, "/")
	if len(splitted) > 1 {
		user := splitted[0]
		searchTerm = strings.Join(splitted[1:], "/") + "+user:" + user
	} else {
		searchTerm = params.RepoName
	}
	url := "https://api.github.com/search/repositories?q=" + searchTerm
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Add("Accept", "application/vnd.github+json")
	request.Header.Add("Authorization", "Bearer "+token)
	request.Header.Add("X-GitHub-Api-Version", "2022-11-28")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return nil, fmt.Errorf("search failed: %d - %s", r.StatusCode, string(b))
	}
	b, _ := io.ReadAll(r.Body)
	results := SearchResults{}
	err = json.Unmarshal(b, &results)

	res := []string{}
	for _, v := range results.Items {
		res = append(res, v.FullName)
	}
	return res, err
}
