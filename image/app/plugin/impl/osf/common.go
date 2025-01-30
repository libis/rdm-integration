// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"strings"
)

type PageResponse struct {
	Data  []Data `json:"data"`
	Links Links  `json:"links"`
}

type DataResponse struct {
	Data Data `json:"data"`
}

type Data struct {
	Id            string        `json:"id"`
	Attributes    Attributes    `json:"attributes"`
	Relationships Relationships `json:"relationships"`
	Links         Links         `json:"links"`
}

type Attributes struct {
	Title             string `json:"title"`
	Extra             Extra  `json:"extra"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Materialized_path string `json:"materialized_path"`
	Guid              string `json:"guid"`
	Size              int64  `json:"size"`
}

type Extra struct {
	Hashes Hashes `json:"hashes"`
}

type Hashes struct {
	Sha256 string `json:"sha256"`
	Md5    string `json:"md5"`
}

type Relationships struct {
	LinkedNodes Linked `json:"linked_nodes"`
	Files       Linked `json:"files"`
}

type Linked struct {
	LinksWithHref LinksWithHref `json:"links"`
}

type LinksWithHref struct {
	Related LinkHref `json:"related"`
}

type LinkHref struct {
	Href string `json:"href"`
}

type Links struct {
	Next     string `json:"next"`
	Download string `json:"download"`
}

type File struct {
	Id       string
	Name     string
	Path     string
	URL      string
	IsDir    bool
	Hash     string
	HashType string
	Size     int64
}

func getPage(ctx context.Context, url, token string) ([]Data, string, error) {
	b, err := query(ctx, url, token)
	if err != nil {
		return nil, "", err
	}
	res := PageResponse{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return nil, "", fmt.Errorf("%s", string(b))
	}
	return res.Data, res.Links.Next, nil
}

func getData(ctx context.Context, url, token string) (Data, error) {
	b, err := query(ctx, url, token)
	if err != nil {
		return Data{}, err
	}
	res := DataResponse{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return Data{}, fmt.Errorf("%s", string(b))
	}
	return res.Data, nil
}

func query(ctx context.Context, url, token string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		request.Header.Add("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func getFiles(ctx context.Context, server, repoName, token string) ([]File, error) {
	url := fmt.Sprintf("%s/v2/nodes/%s/", server, repoName)
	data, err := getData(ctx, url, token)
	if err != nil {
		return nil, err
	}
	return getFilesFrom(ctx, data.Relationships.Files.LinksWithHref.Related.Href, token)
}

func getFilesFrom(ctx context.Context, url, token string) ([]File, error) {
	filesPage, next, err := getPage(ctx, url, token)
	if err != nil {
		return nil, err
	}
	res := append([]Data{}, filesPage...)
	for next != "" {
		filesPage, next, err = getPage(ctx, next, token)
		if err != nil {
			return nil, err
		}
		res = append(res, filesPage...)
	}
	files := []File{}
	urls := []string{}
	for _, v := range res {
		id := strings.TrimPrefix(v.Attributes.Materialized_path, "/")
		path := strings.TrimSuffix(id, v.Attributes.Name)
		path = strings.TrimSuffix(path, "/")
		hashType := ""
		hash := ""
		if v.Attributes.Extra.Hashes.Md5 != "" {
			hashType = types.Md5
			hash = v.Attributes.Extra.Hashes.Md5
		} else if v.Attributes.Extra.Hashes.Sha256 != "" {
			hashType = types.SHA256
			hash = v.Attributes.Extra.Hashes.Sha256
		}
		files = append(files, File{
			Id:       id,
			Name:     v.Attributes.Name,
			Path:     path,
			URL:      v.Links.Download,
			IsDir:    v.Attributes.Kind != "file",
			Hash:     hash,
			HashType: hashType,
			Size:     v.Attributes.Size,
		})
		href := v.Relationships.Files.LinksWithHref.Related.Href
		if href != "" {
			urls = append(urls, href)
		}
	}
	for _, v := range urls {
		moreFiles, err := getFilesFrom(ctx, v, token)
		if err != nil {
			return nil, err
		}
		files = append(files, moreFiles...)
	}
	return files, nil
}
