// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"integration/app/plugin/types"
)

func Search(ctx context.Context, params types.OptionsRequest) ([]string, error) {
	collecions, err := search(ctx, params.Url, params.RepoName, params.Token)
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, c := range collecions {
		res = append(res, c.Attributes.Title)
	}
	return res, nil
}
