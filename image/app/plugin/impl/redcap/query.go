// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"strings"
)

func Query(ctx context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	url := fmt.Sprintf("%s/api/", req.Url)
	entries, err := listEntries(ctx, "", "", url, req.Token)
	if err != nil {
		return nil, err
	}
	folder := strings.TrimPrefix(req.Option, "/")
	folder = strings.TrimSuffix(folder, "/")
	filtered := []Entry{}
	for _, e := range entries {
		if folder == "" || strings.HasPrefix(e.Id, folder) {
			filtered = append(filtered, e)
		}
	}
	return toNodeMap(filtered, nm, url, req.Token)
}

func toNodeMap(entries []Entry, nm map[string]tree.Node, url, token string) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		checkSum, size, err := hash(e, nm, url, token)
		if err != nil {
			return nil, err
		}

		node := tree.Node{
			Id:   e.Id,
			Name: e.Name,
			Path: e.Path,
			Attributes: tree.Attributes{
				URL:            e.DocId,
				ParentId:       e.Path,
				IsFile:         !e.IsDir,
				RemoteHash:     checkSum,
				RemoteHashType: types.Md5,
				Metadata: tree.Metadata{
					Label:          e.Name,
					DirectoryLabel: e.Path,
					DataFile: tree.DataFile{
						Filename:    e.Name,
						ContentType: "application/octet-stream",
						Filesize:    size,
						Checksum: tree.Checksum{
							Type:  types.Md5,
							Value: checkSum,
						},
					},
				},
			},
		}
		res[e.Id] = node
	}
	return res, nil
}

func hash(entry Entry, nm map[string]tree.Node, url, token string) (string, int64, error) {
	if _, ok := nm[entry.Id]; !ok {
		return types.NotNeeded, 0, nil
	}
	data, _ := json.Marshal(Request{
		Token:        token,
		Content:      "fileRepository",
		Action:       "export",
		DocId:        entry.DocId,
		ReturnFormat: "json",
	})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	hasher := md5.New()
	size, err := io.Copy(hasher, resp.Body)
	return fmt.Sprintf("%x", hasher.Sum(nil)), size, err
}
