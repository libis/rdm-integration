// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
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
		if err == nil {
			logging.Logger.Printf("[globus] Endpoint '%s' DefaultDirectory: '%s'", params.RepoName, endpoint.DefaultDirectory)
			params.Option = endpoint.DefaultDirectory
			res, err = doListFolderItems(ctx, params)
			if err == nil && len(res) > 0 {
				logging.Logger.Printf("[globus] Successfully loaded DefaultDirectory, returning %d folders", len(res))
				return res, nil
			} else {
				logging.Logger.Printf("[globus] Failed to load DefaultDirectory (err=%v, count=%d), falling back to root", err, len(res))
				params.Option = ""
			}
		} else {
			logging.Logger.Printf("[globus] Failed to fetch endpoint metadata: %v, will try root", err)
		}
	}
	res, err = doListFolderItems(ctx, params)
	if len(res) == 0 && (err == nil || strings.Contains(err.Error(), "ClientError.NotFound")) && params.Option == "" {
		logging.Logger.Printf("[globus] Root listing empty/not found, trying home directory '/~/'")
		params.Option = "/~/"
		return doListFolderItems(ctx, params)
	}
	return
}

func doListFolderItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
	folder := params.Option
	if folder == "" {
		folder = "/"
	}
	logging.Logger.Printf("[globus] Listing folder: '%s' (endpoint: %s)", folder, params.RepoName)
	items, err := listItems(ctx, folder, params.Url+"/operation/endpoint/"+params.RepoName+"/ls", params.Token, params.User, false)
	res = []types.SelectItem{}
	if err != nil {
		logging.Logger.Printf("[globus] Error listing folder '%s': %v", folder, err)
		return res, err
	}
	logging.Logger.Printf("[globus] Listed folder '%s': found %d items", folder, len(items))
	for _, e := range items {
		if e.IsDir {
			logging.Logger.Printf("[globus]   - Path: %s, ID: %s", e.Path, e.Id)
			res = append(res, types.SelectItem{Label: e.Name, Value: e.Id})
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
