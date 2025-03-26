package onedrive

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("search: missing parameters: expected url, token, got: %+v", params)
	}
	if params.Option != "" {
		return listFolderGraphItems(ctx, params)
	}

	drives, err := getPartialResponse(ctx, params.Url+"/sites?search="+params.RepoName, params.Token)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, d := range drives.Value {
		res = append(res, types.SelectItem{Label: d.Name, Value: d.Id})
	}
	return res, nil
}
