// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"net/url"
	"strings"
)

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url and token, got %+v", params)
	}
	url := fmt.Sprintf("%s/v2/nodes/?filter[title][icontains]=%s", params.Url, url.QueryEscape(strings.ToLower(params.RepoName)))
	if params.RepoName == "" {
		url = fmt.Sprintf("%s/v2/nodes/", params.Url)
	}
	nodes, _, err := getPage(ctx, url, params.Token)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, c := range nodes {
		label := fmt.Sprintf("%s (%s)", c.Attributes.Title, c.Id)
		res = append(res, types.SelectItem{Label: label, Value: c.Id})
	}
	return res, nil
}
