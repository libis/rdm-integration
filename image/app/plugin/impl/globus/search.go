package globus

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %+v", params)
	}
	url := params.Url + "/endpoint_search?filter_fulltext=" + params.RepoName
	endpoints, err := getPartialResponse(ctx, url, params.Token, 10, 0)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, d := range endpoints.Data {
		offline := ""
		if d.EntityType == "GCP_mapped_collection" && !d.GCPConnected {
			offline = " (offline)"
		}
		res = append(res, types.SelectItem{Label: d.DisplayName + offline, Value: d.Id})
	}
	return res, nil
}
