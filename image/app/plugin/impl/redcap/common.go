// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type Request struct {
	Token        string `json:"token"`
	Content      string `json:"content"`
	Action       string `json:"action"`
	Format       string `json:"format"`
	FolderId     string `json:"folder_id"`
	DocId        string `json:"doc_id"`
	ReturnFormat string `json:"returnFormat"`
}

type RedCapResponseEntry struct {
	FolderId string `json:"folder_id"`
	DocId    string `json:"doc_id"`
	Name     string `json:"name"`
}

type Entry struct {
	Path  string
	Id    string
	Name  string
	IsDir bool
	DocId string
}

func listEntries(ctx context.Context, folderId, path, url, token string) ([]Entry, error) {
	data, _ := json.Marshal(Request{
		Token:        token,
		Content:      "fileRepository",
		Action:       "list",
		Format:       "json",
		FolderId:     folderId,
		ReturnFormat: "json",
	})
	request, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
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
		return nil, err
	}
	res := []Entry{}
	sep := "/"
	if path == "" {
		sep = ""
	}
	for _, v := range response {
		isDir := v.FolderId != ""
		id := path + sep + v.Name
		if isDir {
			folderEntries, err := listEntries(ctx, v.FolderId, id, url, token)
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
