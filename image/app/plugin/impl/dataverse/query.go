// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"fmt"
	"github.com/libis/rdm-dataverse-go-api/api"
	"integration/app/plugin/types"
	"integration/app/tree"
)

func Query(ctx context.Context, req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	path := "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + req.RepoName
	client, _ := newClient(req.PluginId, req.Url, req.User, req.Token)
	request := client.NewRequest(path, "GET", nil, nil)
	res := api.ListResponse{}
	err := api.Do(ctx, request, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		return nil, fmt.Errorf("listing files for %s failed: %+v", req.RepoName, res)
	}
	return mapToNodes(res.Data), nil
}

func mapToNodes(data []api.MetaData) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, d := range data {
		dir := ""
		if d.DirectoryLabel != "" {
			dir = d.DirectoryLabel + "/"
		}
		id := dir + d.DataFile.FileName
		hash := d.DataFile.Md5
		hashType := types.Md5
		if hash == "" {
			hash = d.DataFile.Checksum.Value
			hashType = d.DataFile.Checksum.Type
		}
		res[id] = tree.Node{
			Id:   id,
			Name: d.DataFile.FileName,
			Path: d.DirectoryLabel,
			Attributes: tree.Attributes{
				URL:            fmt.Sprintf("/api/v1/access/datafile/%v", d.DataFile.Id),
				IsFile:         true,
				RemoteHash:     hash,
				RemoteHashType: hashType,
				RemoteFilesize: d.DataFile.FileSize,
			},
		}
	}
	return res
}
