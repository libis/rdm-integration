// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"path"
	"strings"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("options: missing parameters: expected url, token, got: %+v", params)
	}
	return listFolderItems(ctx, params)
}

func listFolderItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
	if params.Option == "" {
		endpoint, err := getEndpoint(ctx, params)
		if err == nil && endpoint.DefaultDirectory != "" {
			// Check if DefaultDirectory is a template variable (e.g., "{server_default}")
			// These should be replaced with /~/ which Globus interprets as the user's home directory
			isTemplate := strings.Contains(endpoint.DefaultDirectory, "{") && strings.Contains(endpoint.DefaultDirectory, "}")
			if isTemplate {
				params.Option = "/~/"
			} else {
				params.Option = endpoint.DefaultDirectory
			}
			res, err = doListFolderItems(ctx, params, "")
			if err == nil && len(res) > 0 {
				return res, nil
			} else if !isTemplate {
				// DefaultDirectory is empty or failed, list its parent so user can see/select it
				// Only do this for non-template directories (we know the exact path)
				// For templates, we can't know the resolved path to pre-select
				parentDir := path.Dir(strings.TrimSuffix(endpoint.DefaultDirectory, "/"))
				if parentDir != "" && parentDir != "." {
					params.Option = parentDir + "/"
					// Pass the default directory so it can be marked as selected
					return doListFolderItems(ctx, params, endpoint.DefaultDirectory)
				}
				params.Option = ""
			}
		}
	}
	res, err = doListFolderItems(ctx, params, "")
	if len(res) == 0 && (err == nil || strings.Contains(err.Error(), "ClientError.NotFound")) && params.Option == "" {
		params.Option = "/~/"
		return doListFolderItems(ctx, params, "")
	}
	return
}

func doListFolderItems(ctx context.Context, params types.OptionsRequest, defaultDir string) (res []types.SelectItem, err error) {
	folder := params.Option
	if folder == "" {
		folder = "/"
	}
	items, err := listItems(ctx, folder, params.Url+"/operation/endpoint/"+params.RepoName+"/ls", params.Token, params.User, false)
	res = []types.SelectItem{}
	if err != nil {
		return res, err
	}
	for _, e := range items {
		if e.IsDir {
			selected := defaultDir != "" && e.Id == defaultDir
			res = append(res, types.SelectItem{Label: e.Name, Value: e.Id, Selected: selected})
		}
	}
	return res, nil
}

func getEndpoint(ctx context.Context, params types.OptionsRequest) (Response, error) {
	url := params.Url + "/endpoint/" + params.RepoName
	res, err := DoGlobusRequest(ctx, url, "GET", params.Token, nil)
	if err != nil {
		return Response{}, err
	}
	response := Response{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return Response{}, err
	}
	return response, nil
}
