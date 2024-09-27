// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %+v", params)
	}
	return listFolderItems(ctx, params)
}

func listFolderItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
	folder := params.Option
	if folder == "" {
		folder = "/"
	}
	items, err := listItems(ctx, folder, params.Url+"/operation/endpoint/"+params.RepoName+"/ls", params.Token, false)
	res = []types.SelectItem{}
	if err != nil {
		logging.Logger.Printf("globus plugin err: %v\n", err)
		return res, nil // errors break the gui dropdown; most likely the path is a file, not a folder
	}
	for _, e := range items {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: e.Name, Value: e.Id})
		}
	}
	return res, nil
}
