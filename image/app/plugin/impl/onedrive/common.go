// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package onedrive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

type Response struct {
	Value []GraphItem `json:"value"`
	Next  string      `json:"@odata.nextLink"`
}

type GraphItem struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Folder Folder `json:"folder"`
	File   File   `json:"file"`
	Size   int64  `json:"size"`
	Url    string `json:"@microsoft.graph.downloadUrl"`
}

type Folder struct {
	ChildCount int64 `json:"childCount"`
}

type File struct {
	MimeType string `json:"mimeType"`
	Hashes   Hashes `json:"hashes"`
}

type Hashes struct {
	Sha1Hash     string `json:"sha1Hash"`
	Sha256Hash   string `json:"sha256Hash"`
	QuickXorHash string `json:"quickXorHash"`
}

type Entry struct {
	Id       string
	Name     string
	Path     string
	URL      string
	IsDir    bool
	Hash     string
	HashType string
	Size     int64
}

func listGraphItems(ctx context.Context, path, url, token string, recursive bool) ([]Entry, error) {
	folder := path
	if path != "" {
		folder = ":/" + path + ":"
	}
	response, err := getResponse(ctx, url+folder+"/children", token)
	if err != nil {
		return nil, err
	}
	res := []Entry{}
	sep := "/"
	if path == "" {
		sep = ""
	}
	for _, v := range response {
		isDir := !(v.File.Hashes.Sha1Hash != "" || v.File.Hashes.Sha256Hash != "" || v.File.Hashes.QuickXorHash != "")
		id := path + sep + v.Name
		if recursive && isDir && v.Folder.ChildCount > 0 {
			folderEntries, err := listGraphItems(ctx, id, url, token, true)
			if err != nil {
				return nil, err
			}
			res = append(res, folderEntries...)
		}
		hashType := ""
		hash := ""
		if v.File.Hashes.Sha256Hash != "" {
			hashType = types.SHA256
			hash = v.File.Hashes.Sha256Hash
		} else if v.File.Hashes.Sha1Hash != "" {
			hashType = types.SHA1
			hash = v.File.Hashes.Sha1Hash
		} else if v.File.Hashes.QuickXorHash != "" {
			hashType = types.QuickXorHash
			hashBytes, _ := base64.StdEncoding.DecodeString(v.File.Hashes.QuickXorHash)
			hash = fmt.Sprintf("%x", hashBytes)
		}
		res = append(res, Entry{
			Path:     path,
			Id:       id,
			Name:     v.Name,
			IsDir:    isDir,
			URL:      v.Url,
			HashType: hashType,
			Hash:     hash,
			Size:     v.Size,
		})
	}
	return res, nil
}

func getResponse(ctx context.Context, url string, token string) ([]GraphItem, error) {
	next := url
	res := []GraphItem{}
	for next != "" {
		response, err := getParialResponse(ctx, next, token)
		if err != nil {
			return nil, err
		}
		res = append(res, response.Value...)
		next = response.Next
	}
	return res, nil
}

func getParialResponse(ctx context.Context, url string, token string) (Response, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Response{}, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return Response{}, err
	}
	response := Response{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return Response{}, fmt.Errorf(string(b))
	}
	return response, nil
}
