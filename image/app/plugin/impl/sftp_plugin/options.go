// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package sftp_plugin

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

func Options(_ context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	user := params.User
	password := params.Token
	server := params.Url
	if user == "" || password == "" || server == "" {
		return nil, fmt.Errorf("folders: missing parameters: expected url, user and password, got: %+v", params)
	}

	cl, err := getClient(server, user, password)
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	dir := params.Option
	if dir == "" {
		dir = "/"
	}
	return getDirs(cl, dir)
}

func getDirs(cl *client, dir string) ([]types.SelectItem, error) {
	entries, err := cl.SftpClient.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	for _, v := range entries {
		if v.IsDir() {
			res = append(res, types.SelectItem{Label: v.Name(), Value: dir + v.Name() + "/"})
		}
	}
	return res, nil
}
