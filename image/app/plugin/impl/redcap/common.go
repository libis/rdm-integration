// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Request struct {
	Token        string `json:"token"`
	Content      string `json:"content"`
	Action       string `json:"action"`
	Format       string `json:"format"`
	FolderId     int64  `json:"folder_id"`
	DocId        int64  `json:"doc_id"`
	ReturnFormat string `json:"returnFormat"`
}

type RedCapResponseEntry struct {
	FolderId int64  `json:"folder_id"`
	DocId    int64  `json:"doc_id"`
	Name     string `json:"name"`
}

type Entry struct {
	Path  string
	Id    string
	Name  string
	IsDir bool
	DocId int64
}

func listEntries(ctx context.Context, folderId int64, path, url, token string, recursive bool) ([]Entry, error) {
	data := Request{
		Token:        token,
		Content:      "fileRepository",
		Action:       "list",
		Format:       "json",
		FolderId:     folderId,
		ReturnFormat: "json",
	}
	request, err := http.NewRequestWithContext(ctx, "POST", url, encode(data))
	if err != nil {
		return nil, err
	}
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Add("Accept", "application/json")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	response := []RedCapResponseEntry{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return nil, fmt.Errorf(string(b))
	}
	res := []Entry{}
	sep := "/"
	if path == "" {
		sep = ""
	}
	for _, v := range response {
		isDir := v.FolderId != 0
		id := path + sep + v.Name
		if isDir && recursive {
			folderEntries, err := listEntries(ctx, v.FolderId, id, url, token, true)
			if err != nil {
				return nil, err
			}
			res = append(res, folderEntries...)
		}
		res = append(res, Entry{
			Path:  path,
			Id:    id,
			Name:  v.Name,
			IsDir: isDir,
			DocId: v.DocId,
		})
	}
	return res, nil
}

func encode(req Request) *bytes.Buffer {
	folderId := ""
	if req.FolderId != 0 {
		folderId = fmt.Sprint(req.FolderId)
	}
	docId := ""
	if req.DocId != 0 {
		docId = fmt.Sprint(req.DocId)
	}
	return bytes.NewBuffer([]byte(
		fmt.Sprintf("token=%s&content=%s&action=%s&format=%s&folder_id=%s&doc_id=%s&returnFormat=%s",
			url.QueryEscape(req.Token),
			url.QueryEscape(req.Content),
			url.QueryEscape(req.Action),
			url.QueryEscape(req.Format),
			folderId,
			docId,
			url.QueryEscape(req.ReturnFormat),
		)))
}
