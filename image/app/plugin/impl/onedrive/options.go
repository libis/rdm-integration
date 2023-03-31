// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package onedrive

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"strings"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %+v", params)
	}
	drives, err := getResponse(ctx, params.Url+"/me/drives", params.Token)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	if params.Option != "" {
		return listFolderGrapthItems(ctx, params, drives)
	}
	for _, d := range drives {
		res = append(res, types.SelectItem{Label: d.Name + "/", Value: d.Id})
	}
	return res, nil
}

func listFolderGrapthItems(ctx context.Context, params types.OptionsRequest, drives []GraphItem) (res []types.SelectItem, err error) {
	s := strings.Split(params.Option, "/")
	folder := ""
	if len(s) > 1 {
		folder = strings.Join(s[1:], "/")
	}
	if folder == "/" {
		folder = ""
	}
	d := GraphItem{}
	for _, gi := range drives {
		if gi.Id == s[0] {
			d = gi
			break
		}
	}
	items, err := listGraphItems(ctx, folder, params.Url+"/drives/"+d.Id+"/root", params.Token, false)
	if err != nil {
		return nil, err
	}
	for _, e := range items {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: d.Name + "/" + e.Id, Value: d.Id + "/" + e.Id})
		}
	}
	return res, nil
}
