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
	"sync"
	"time"
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

var (
	httpClient *http.Client
	clientOnce sync.Once
)

func getHTTPClient() *http.Client {
	clientOnce.Do(func() {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		}
	})
	return httpClient
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

	r, err := getHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(r.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", r.StatusCode, string(body))
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	response := []RedCapResponseEntry{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
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
	params := url.Values{}
	params.Set("token", req.Token)
	params.Set("content", req.Content)
	params.Set("action", req.Action)
	params.Set("format", req.Format)
	params.Set("returnFormat", req.ReturnFormat)

	if req.FolderId != 0 {
		params.Set("folder_id", fmt.Sprint(req.FolderId))
	}
	if req.DocId != 0 {
		params.Set("doc_id", fmt.Sprint(req.DocId))
	}

	return bytes.NewBufferString(params.Encode())
}
