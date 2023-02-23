// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package onedrive

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %v", params)
	}
	drives, err := getResponse(ctx, params.Url+"/me/drives", params.Token)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, d := range drives {
		items, err := listGraphItems(ctx, "/drives/"+d.Id+"/root:/", "", params.Url, params.Token)
		if err != nil {
			return nil, err
		}
		res = append(res, types.SelectItem{Label: d.Name + "/", Value: d.Id})
		for _, e := range items {
			if e.IsDir {
				res = append(res, types.SelectItem{Label: d.Name + "/" + e.Id, Value: d.Id + "/" + e.Id})
			}
		}
	}
	return res, nil
}
