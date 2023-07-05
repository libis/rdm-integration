// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"github.com/libis/rdm-dataverse-go-api/api"
	"integration/app/plugin/types"
)

type SearchResults struct {
	Items []Item `json:"items"`
}

type Item struct {
	FullName string `json:"full_name"`
}

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	path := "/api/v1/search?" +
		"type=dataset" +
		"&q=" + params.RepoName
	client := NewClient(params.PluginId, params.Url, params.User, params.Token)

	retrieveResponse := api.RetrieveResponse{}
	req := client.NewRequest(path, "GET", nil, nil)
	err := api.Do(ctx, req, &retrieveResponse)
	if err != nil {
		return nil, err
	}

	res := []types.SelectItem{}
	added := map[string]bool{}
	for _, v := range retrieveResponse.Data.Items {
		name := v.Name + " (" + v.GlobalId + ")"
		if !added[name] {
			added[name] = true
			res = append(res, types.SelectItem{Label: name, Value: v.GlobalId})
		}
	}
	return res, err
}
