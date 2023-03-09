// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package onedrive

import (
	"context"
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"strings"
)

func Query(ctx context.Context, req types.CompareRequest, nm map[string]tree.Node) (map[string]tree.Node, error) {
	s := strings.Split(req.Option, "/")
	folder := ""
	if len(s) > 1 {
		folder = strings.Join(s[1:], "/")
	}
	if folder == "/" {
		folder = ""
	}
	entries, err := listGraphItems(ctx, folder, req.Url+"/drives/"+s[0]+"/root", req.Token)
	if err != nil {
		return nil, err
	}
	return toNodeMap(entries, nm, req.Url, req.Token)
}

func toNodeMap(entries []Entry, nm map[string]tree.Node, url, token string) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		hashType, hash, err := hash(e, nm, token)
		if err != nil {
			return nil, err
		}

		node := tree.Node{
			Id:   e.Id,
			Name: e.Name,
			Path: e.Path,
			Attributes: tree.Attributes{
				URL:            e.URL,
				IsFile:         !e.IsDir,
				RemoteHash:     hash,
				RemoteHashType: hashType,
				RemoteFilesize: e.Size,
			},
		}
		res[e.Id] = node
	}
	return res, nil
}

func hash(entry Entry, nm map[string]tree.Node, token string) (string, string, error) {
	if entry.HashType != "" {
		return entry.HashType, entry.Hash, nil
	}
	if _, ok := nm[entry.Id]; !ok {
		return types.Md5, types.NotNeeded, nil
	}
	req, _ := http.NewRequest("GET", entry.URL, nil)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	hasher := md5.New()
	_, err = io.Copy(hasher, resp.Body)
	return types.Md5, fmt.Sprintf("%x", hasher.Sum(nil)), err
}
