// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"

	"github.com/cyverse/go-irodsclient/fs"
)

func Query(_ context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	cl, err := NewIrodsClient(req.Url, req.RepoName, req.User, req.Token)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	entries, err := cl.FileSystem.List(req.Option)
	if err != nil {
		return nil, err
	}
	return toNodeMap(cl, req.Option, entries, nm)
}

func toNodeMap(cl *IrodsClient, folder string, entries []*fs.Entry, nm map[string]tree.Node) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	dirs := []string{}
	for _, e := range entries {
		id := e.Path[len(folder)+1:]
		isFile := e.Type == "file"
		if !isFile {
			if e.Type == "directory" {
				dirs = append(dirs, e.Path)
			}
			continue
		}

		parentId := ""
		ancestors := strings.Split(id, "/")
		fileName := id
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		hashType, h, err := hash(cl, folder, id, nm)
		if err != nil {
			return nil, err
		}
		node := tree.Node{
			Id:   id,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				IsFile:         isFile,
				RemoteHash:     h,
				RemoteHashType: hashType,
				RemoteFileSize: e.Size,
			},
		}
		res[id] = node
	}
	for _, d := range dirs {
		subEntries, err := cl.FileSystem.List(d)
		if err != nil {
			return nil, err
		}
		irodsNm, err := toNodeMap(cl, folder, subEntries, nm)
		if err != nil {
			return nil, err
		}
		for k, v := range irodsNm {
			res[k] = v
		}
	}
	return res, nil
}

func hash(cl *IrodsClient, folder, path string, nm map[string]tree.Node) (string, string, error) {
	if _, ok := nm[path]; !ok {
		return types.Md5, types.NotNeeded, nil
	}
	return cl.Checksum(folder + "/" + path)
}
