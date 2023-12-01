// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package onedrive

import (
	"context"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"strings"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %+v", params)
	}
	if params.Option != "" {
		return listFolderGrapthItems(ctx, params)
	}
	drives, err := getResponse(ctx, params.Url+"/me/drives", params.Token)
	if err != nil {
		return nil, err
	}
	siteDrives, err := listSiteDrives(ctx, params)
	if err != nil {
		return nil, err
	}
	drives = append(drives, siteDrives...)
	res := []types.SelectItem{}
	for _, d := range drives {
		res = append(res, types.SelectItem{Label: d.Name, Value: d.Id})
	}
	return res, nil
}

func listSiteDrives(ctx context.Context, params types.OptionsRequest) (res []GraphItem, err error) {
	sites, err := getResponse(ctx, params.Url+"/sites?search=", params.Token)
	if err != nil {
		return nil, err
	}
	for _, s := range sites {
		url := params.Url + "/sites/" + s.Id + "/drives"
		drives, err := getResponse(ctx, url, params.Token)
		if err != nil {
			fmt.Println(err)
		}
		for _, d := range drives {
			d.Name = s.Name + ": " + d.Name
			res = append(res, d)
		}
	}
	return res, nil
}

func listFolderGrapthItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
	s := strings.Split(params.Option, "/")
	folder := ""
	if len(s) > 1 {
		folder = strings.Join(s[1:], "/")
	}
	if folder == "/" {
		folder = ""
	}
	items, err := listGraphItems(ctx, folder, params.Url+"/drives/"+s[0]+"/root", params.Token, false)
	res = []types.SelectItem{}
	if err != nil {
		logging.Logger.Printf("onedrive plugin err: %v\n", err)
		return res, nil // errors break the gui dropdown; most likely the path is a file, not a folder
	}
	for _, e := range items {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: e.Name, Value: s[0] + "/" + e.Id})
		}
	}
	return res, nil
}
