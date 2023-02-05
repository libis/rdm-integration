// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

type Item struct {
	PathWithBamespace string `json:"path_with_namespace"`
}

func Search(ctx context.Context, params types.OptionsRequest) ([]string, error) {
	token := params.Token
	if token == "" {
		return nil, fmt.Errorf("not authorized")
	}
	url := params.Url + "/api/v4/search?scope=projects&search=" + params.RepoName
	request, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}
	defer r.Body.Close()
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
