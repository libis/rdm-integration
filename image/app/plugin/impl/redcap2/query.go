// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sort"
	"strings"
)

func Query(ctx context.Context, req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	if req.Url == "" || req.Token == "" {
		return nil, fmt.Errorf("query: missing parameters: expected url, token")
	}

	opts, err := parsePluginOptions(req.PluginOptions)
	if err != nil {
		return nil, err
	}

	reportID := opts.ReportID
	if reportID == "" {
		reportID = strings.TrimSpace(req.Option)
	}
	if reportID == "" && opts.ExportMode != "records" {
		return nil, fmt.Errorf("query: missing report id")
	}
	opts.ReportID = reportID

	bundle, err := cachedBuildExportBundle(ctx, req.Url, req.Token, opts, reportID)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(bundle.Files))
	for path := range bundle.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	nodes := make(map[string]tree.Node, len(paths))
	for _, fullPath := range paths {
		data := bundle.Files[fullPath]
		parentPath, fileName := splitPath(fullPath)
		nodes[fullPath] = tree.Node{
			Id:   fullPath,
			Name: fileName,
			Path: parentPath,
			Attributes: tree.Attributes{
				URL:            fullPath,
				IsFile:         true,
				RemoteHash:     md5Hex(data),
				RemoteHashType: types.Md5,
				RemoteFileSize: int64(len(data)),
			},
		}
	}
	return nodes, nil
}

func splitPath(path string) (parent string, name string) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return "", ""
	}
	idx := strings.LastIndex(clean, "/")
	if idx < 0 {
		return "", clean
	}
	parent = clean[:idx]
	name = clean[idx+1:]
	return parent, name
}
