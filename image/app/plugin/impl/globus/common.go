// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Response struct {
	Code             string   `json:"code"`
	Message          string   `json:"message"`
	RequiredScopes   []string `json:"required_scopes"`
	Data             []Data   `json:"DATA"`
	HasNextPage      bool     `json:"has_next_page"`
	Limit            int      `json:"limit"`
	Offset           int      `json:"offset"`
	DefaultDirectory string   `json:"default_directory"`
	AbsolutePath     string   `json:"absolute_path"`
}

type Data struct {
	DisplayName  string      `json:"display_name"`
	EntityType   string      `json:"entity_type"`
	GCPConnected bool        `json:"gcp_connected"`
	Id           interface{} `json:"id"` // Can be string or number in Globus API response
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	LinkTarget   string      `json:"link_target"`
	LastModified string      `json:"last_modified"`
	Size         int64       `json:"size"`
	AbsolutePath string      `json:"absolute_path"`
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

func listItems(ctx context.Context, path, theUrl, token, user string, recursive bool) ([]Entry, error) {
	urlString := theUrl + "?path=" + url.QueryEscape(path)
	response, err := getResponse(ctx, urlString, token)
	if err != nil {
		return nil, err
	}
	res := []Entry{}
	for _, v := range response {
		isDir := v.Type == "dir"

		// Use the API's resolved absolute_path which handles /~/, /{server_default}/, etc.
		// Fall back to queried path only if absolute_path is empty (edge case).
		basePath := v.AbsolutePath
		if basePath == "" {
			basePath = path
		}
		if basePath == "" {
			basePath = "/"
		}
		// Ensure basePath ends with /
		if !strings.HasSuffix(basePath, "/") {
			basePath = basePath + "/"
		}

		id := basePath + v.Name + "/"
		if recursive && isDir {
			folderEntries, err := listItems(ctx, id, theUrl, token, user, true)
			if err != nil {
				return nil, err
			}
			res = append(res, folderEntries...)
		}
		res = append(res, Entry{
			Path:     basePath,
			Id:       id,
			Name:     v.Name,
			IsDir:    isDir,
			URL:      v.LinkTarget,
			Hash:     v.LastModified,
			HashType: types.LastModified,
			Size:     v.Size,
		})
	}
	return res, nil
}

func getResponse(ctx context.Context, url string, token string) ([]Data, error) {
	next := true
	res := []Data{}
	for i := 0; next; i++ {
		response, err := getPartialResponse(ctx, url, token, 100, i)
		if err != nil {
			return nil, err
		}
		for _, r := range response.Data {
			r.AbsolutePath = response.AbsolutePath
			res = append(res, r)
		}
		next = response.HasNextPage
	}
	return res, nil
}

func getPartialResponse(ctx context.Context, url string, token string, limit, offset int) (Response, error) {
	res, err := doGetPartialResponse(ctx, url, token, limit, offset)
	if err != nil && strings.HasPrefix(err.Error(), "ConsentRequired") {
		return res, fmt.Errorf("*scopes*%v*scopes*", strings.Join(res.RequiredScopes, " "))
	}
	return res, err
}

func doGetPartialResponse(ctx context.Context, url string, token string, limit, offset int) (Response, error) {
	fullUrl := fmt.Sprintf("%v&limit=%v&offset=%v", url, limit, offset)
	b, err := DoGlobusRequest(ctx, fullUrl, "GET", token, nil)
	if err != nil {
		return Response{}, err
	}
	response := Response{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return Response{}, fmt.Errorf("globus error: response could not be unmarshalled from %v", string(b))
	}
	if response.Code != "" && response.Message != "" {
		return response, fmt.Errorf("%v: %v", response.Code, response.Message)
	}
	return response, nil
}

func DoGlobusRequest(ctx context.Context, url, method, token string, body io.Reader) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
