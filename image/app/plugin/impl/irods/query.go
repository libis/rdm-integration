// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"encoding/binary"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"

	"github.com/cyverse/go-irodsclient/fs"
)

func Query(req types.CompareRequest, _ map[string]tree.Node) (map[string]tree.Node, error) {
	cl, err := NewIrodsClient(req.Url, req.RepoName, req.User, req.Token)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	entries, err := cl.FileSystem.List(req.Option)
	if err != nil {
		return nil, err
	}
	return toNodeMap(cl, req.Option, entries)
}

func toNodeMap(cl *IrodsClient, folder string, entries []*fs.Entry) (map[string]tree.Node, error) {
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
		hash := e.CheckSum
		hashType := types.Md5
		if hash == "" {
			h := make([]byte, 8)
			binary.LittleEndian.PutUint64(h, uint64(e.Size))
			hash = fmt.Sprintf("%x", h)
			hashType = types.FileSize
		}
		node := tree.Node{
			Id:   id,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     hash,
				RemoteHashType: hashType,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    e.Size,
						Checksum: tree.Checksum{
							Type:  hashType,
							Value: hash,
						},
					},
				},
			},
		}
		res[id] = node
	}
	for _, d := range dirs {
		subEntries, err := cl.FileSystem.List(d)
		if err != nil {
			return nil, err
		}
		irodsNm, err := toNodeMap(cl, folder, subEntries)
		for k, v := range irodsNm {
			res[k] = v
		}
	}
	return res, nil
}
