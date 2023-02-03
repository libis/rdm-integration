// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package gitlab

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"strings"
)

type Item struct {
	PathWithBamespace string `json:"path_with_namespace"`
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
		searchTerm = "groups/" + user + "/search?scope=projects&search=" + strings.Join(splitted[1:], "/")
	} else {
		searchTerm = "search?scope=projects&search=" + params.RepoName
	}
	url := params.Url + "/api/v4/" + searchTerm
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}
	if r.StatusCode != 200 && r.StatusCode != 404 {
		b, _ := io.ReadAll(r.Body)
		return nil, fmt.Errorf("search failed: %d - %s", r.StatusCode, string(b))
	}
	b, _ := io.ReadAll(r.Body)
	results := []Item{}
	json.Unmarshal(b, &results)

	res := []string{}
	for _, v := range results {
		res = append(res, v.PathWithBamespace)
	}
	return res, err
}
