// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"context"
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"strconv"
)

func Query(ctx context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	url := fmt.Sprintf("%s/api/", req.Url)
	folderId, err := strconv.Atoi(req.Option)
	if err != nil {
		return nil, err
	}
	entries, err := listEntries(ctx, int64(folderId), "", url, req.Token, true)
	if err != nil {
		return nil, err
	}
	return toNodeMap(entries, nm, url, req.Token)
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
				URL:            fmt.Sprint(e.DocId),
				IsFile:         !e.IsDir,
				RemoteHash:     checkSum,
				RemoteHashType: types.Md5,
				RemoteFileSize: size,
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
	data := Request{
		Token:        token,
		Content:      "fileRepository",
		Action:       "export",
		DocId:        entry.DocId,
		ReturnFormat: "json",
	}
	req, _ := http.NewRequest("POST", url, encode(data))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	hasher := md5.New()
	size, err := io.Copy(hasher, resp.Body)
	return fmt.Sprintf("%x", hasher.Sum(nil)), size, err
}
