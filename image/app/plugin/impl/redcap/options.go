// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"strconv"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token")
	}
	if params.Option == "" {
		return []types.SelectItem{{Label: "/", Value: "0"}}, nil
	}
	folderId, err := strconv.Atoi(params.Option)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/api/", params.Url)
	entries, err := listEntries(ctx, int64(folderId), "", url, params.Token, false)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, e := range entries {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: e.Id, Value: fmt.Sprint(e.DocId)})
		}
	}
	return res, nil
}
