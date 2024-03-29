// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"sort"
)

func Options(_ context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	user := params.User
	password := params.Token
	server := params.Url
	zone := params.RepoName
	if user == "" || password == "" || server == "" || zone == "" {
		return nil, fmt.Errorf("folders: missing parameters: expected server, zone, user and password, got: %+v", params)
	}
	cl, err := NewIrodsClient(server, zone, user, password)
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	path := params.Option
	if path == "" {
		path = "/" + zone + "/" + path
	}
	res, err := getDirs(cl, path)
	if err != nil {
		return nil, err
	}

	sort.Strings(res)
	sItems := []types.SelectItem{}
	for _, v := range res {
		sItems = append(sItems, types.SelectItem{Label: v, Value: v})
	}
	return sItems, nil
}

func getDirs(cl *IrodsClient, dir string) ([]string, error) {
	entries, err := cl.FileSystem.List(dir)
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, v := range entries {
		if v.Type == "directory" {
			res = append(res, v.Path)
		}
	}
	return res, nil
}
