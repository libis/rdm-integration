// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]string, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %v", params)
	}
	url := fmt.Sprintf("%s/api/", params.Url)
	entries, err := listEntries(ctx, 0, "", url, params.Token)
	if err != nil {
		return nil, err
	}
	res := []string{"/"}
	for _, e := range entries {
		if e.IsDir {
			res = append(res, e.Id)
		}
	}
	return res, nil
}
