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
	if params.Option != "" {
		// User expanded a specific node — return flat children for that folder
		return doListFolderItems(ctx, params)
	}

	// Initial load: resolve default directory and build nested hierarchy
	endpoint, err := getEndpoint(ctx, params)
	if err != nil || endpoint.DefaultDirectory == "" {
		// No endpoint info or no default directory — try root, then /~/
		params.Option = "/"
		res, err = doListFolderItems(ctx, params)
		if len(res) == 0 && (err == nil || strings.Contains(err.Error(), "ClientError.NotFound")) {
			params.Option = "/~/"
			return doListFolderItems(ctx, params)
		}
		return
	}

	// Resolve template variables like {server_default}
	defaultDir := endpoint.DefaultDirectory
	if strings.Contains(defaultDir, "{") && strings.Contains(defaultDir, "}") {
		defaultDir = "/~/"
	}

	// Resolve the actual absolute path by listing the default directory.
	// Globus resolves /~/ and similar to real paths and returns absolute_path.
	resolvedDir := defaultDir
	params.Option = defaultDir
	defaultDirItems, listErr := doListFolderItems(ctx, params)
	if listErr == nil && len(defaultDirItems) > 0 {
		// The Value of child items is like "/resolved/path/childname/"
		// Extract the resolved parent from the first child's Value
		firstChildPath, ok := defaultDirItems[0].Value.(string)
		if ok {
			parent := path.Dir(strings.TrimSuffix(firstChildPath, "/"))
			if parent != "" && parent != "." {
				resolvedDir = parent + "/"
			}
		}
	} else if listErr != nil && !strings.Contains(listErr.Error(), "ClientError.NotFound") {
		return nil, listErr
	}

	// Build the nested hierarchy from "/" down to the resolved default directory
	return buildHierarchy(resolvedDir, defaultDirItems), nil
}

// buildHierarchy constructs a nested tree of SelectItems from the root "/"
// down to targetDir, with targetDir's children populated.
func buildHierarchy(targetDir string, children []types.SelectItem) []types.SelectItem {
	// Normalize: ensure targetDir starts and ends with /
	targetDir = strings.TrimSuffix(targetDir, "/") + "/"
	if !strings.HasPrefix(targetDir, "/") {
		targetDir = "/" + targetDir
	}

	// Split the path into segments: "/home/user/data/" -> ["home", "user", "data"]
	trimmed := strings.Trim(targetDir, "/")
	if trimmed == "" {
		// Target is root "/" itself — just return the children flat
		return children
	}
	segments := strings.Split(trimmed, "/")

	// Build from the deepest node (the target) upward
	// The target node: selected, expanded, with its children populated
	targetLabel := segments[len(segments)-1]
	targetNode := types.SelectItem{
		Label:    targetLabel,
		Value:    targetDir,
		Selected: true,
		Expanded: true,
		Children: children,
	}

	// Wrap each ancestor around it, from inside out
	current := targetNode
	for i := len(segments) - 2; i >= 0; i-- {
		// Build the path for this ancestor
		ancestorPath := "/" + strings.Join(segments[:i+1], "/") + "/"
		current = types.SelectItem{
			Label:    segments[i],
			Value:    ancestorPath,
			Expanded: true,
			Children: []types.SelectItem{current},
		}
	}

	// The outermost node is under root "/"
	return []types.SelectItem{current}
}

func doListFolderItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
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
