package irods

import (
	"fmt"
	"integration/app/plugin/types"
	"sort"
)

func Options(params types.OptionsRequest) ([]string, error) {
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

	res, err := getDirs(cl, "/"+zone+"/")
	if err != nil {
		return nil, err
	}

	sort.Strings(res)
	return res, nil
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
			subdirs, err := getDirs(cl, v.Path)
			if err != nil {
				return nil, err
			}
			res = append(res, subdirs...)
		}
	}
	return res, nil
}
