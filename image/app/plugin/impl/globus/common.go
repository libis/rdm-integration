// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

type Response struct {
	Data        []Data `json:"DATA"`
	HasNextPage bool   `json:"has_next_page"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type Data struct {
	DisplayName  string `json:"display_name"`
	EntityType   string `json:"entity_type"`
	GCPConnected bool   `json:"gcp_connected"`
	Id           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	LinkTarget   string `json:"link_target"`
	LastModified string `json:"last_modified"`
	Size         int64  `json:"size"`
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

func listItems(ctx context.Context, path, url, token string, recursive bool) ([]Entry, error) {
	response, err := getResponse(ctx, url+"?path="+path, token)
	if err != nil {
		return nil, err
	}
	res := []Entry{}
	for _, v := range response {
		isDir := v.Type == "dir"
		id := path + v.Name + "/"
		if recursive && isDir {
			folderEntries, err := listItems(ctx, id, url, token, true)
			if err != nil {
				return nil, err
			}
			res = append(res, folderEntries...)
		}
		res = append(res, Entry{
			Path:     path,
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
		res = append(res, response.Data...)
		next = response.HasNextPage
	}
	return res, nil
}

func getPartialResponse(ctx context.Context, url string, token string, limit, offset int) (Response, error) {
	b, err := DoGlobusRequest(ctx, url+fmt.Sprintf("&limit=%v&offset=%v", limit, offset), "GET", token, nil)
	if err != nil {
		return Response{}, err
	}
	response := Response{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return Response{}, fmt.Errorf("globus error: Response could not be unmarshalled from %v", string(b))
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
