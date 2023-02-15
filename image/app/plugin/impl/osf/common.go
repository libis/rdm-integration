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

type OSFResponseEntry struct {
	Data  []Data `json:"data"`
	Links Links  `json:"links"`
	Meta  Meta   `json:"meta"`
}

type Data struct {
	Id            string        `json:"id"`
	Type          string        `json:"type"`
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
	LinksWithMeta LinksWithMeta `json:"links"`
}

type LinksWithMeta struct {
	Self    LinkWithMeta `json:"self"`
	Related LinkWithMeta `json:"related"`
}

type LinkWithMeta struct {
	Href string `json:"href"`
	Meta Meta   `json:"meta"`
}

type Links struct {
	Html     string `json:"html"`
	Self     string `json:"self"`
	First    string `json:"first"`
	Last     string `json:"last"`
	Next     string `json:"next"`
	Prev     string `json:"prev"`
	Download string `json:"download"`
}

type Meta struct {
	Total   int64   `json:"total"`
	PerPage int64   `json:"per_page"`
	Version float64 `json:"version"`
}

type Node struct {
	Id       string
	Name     string
	Path     string
	URL      string
	IsDir    bool
	Hash     string
	HashType string
	Size     int64
}

func search(ctx context.Context, server, repoName, token string) ([]Data, error) {
	if server == "" || token == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url and token")
	}
	url := fmt.Sprintf("%s/v2/collections/?filter[title][icontains]=%s", server, repoName)
	if repoName == "" {
		url = fmt.Sprintf("%s/v2/collections/", repoName)
	}
	collections, err := getData(ctx, url, repoName)
	if err != nil {
		return nil, err
	}
	res := collections
	if repoName != "" {
		collections, err := getData(ctx, url, "")
		if err != nil {
			return nil, err
		}
		res = append(res, collections...)
	}
	return res, nil
}

func getData(ctx context.Context, url, token string) ([]Data, error) {
	res, next, err := getPage(ctx, url, token)
	if err != nil {
		return nil, err
	}
	for next != "" {
		res, next, err = getPage(ctx, url, token)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func getPage(ctx context.Context, url, token string) ([]Data, string, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	if token != "" {
		request.Header.Add("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	res := OSFResponseEntry{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return nil, "", fmt.Errorf(string(b))
	}
	return res.Data, res.Links.Next, nil
}

func getNodes(ctx context.Context, url, repoName, token string) ([]Node, error) {
	collections, err := search(ctx, url, repoName, token)
	if err != nil {
		return nil, err
	}
	if len(collections) < 1 {
		return nil, fmt.Errorf("collection \"%v\" not found", repoName)
	}

	res, next, err := getPage(ctx, collections[0].Relationships.Files.LinksWithMeta.Related.Href, token)
	if err != nil {
		return nil, err
	}
	for next != "" {
		res, next, err = getPage(ctx, url, token)
		if err != nil {
			return nil, err
		}
	}
	nodes := []Node{}
	for _, n := range res {
		id := strings.TrimPrefix(n.Attributes.Materialized_path, "/")
		path := strings.TrimSuffix(id, n.Attributes.Name)
		path = strings.TrimSuffix(path, "/")
		hashType := ""
		hash := ""
		if n.Attributes.Extra.Hashes.Md5 != "" {
			hashType = types.Md5
			hash = n.Attributes.Extra.Hashes.Md5
		} else if n.Attributes.Extra.Hashes.Sha256 != "" {
			hashType = types.SHA256
			hash = n.Attributes.Extra.Hashes.Sha256
		}
		nodes = append(nodes, Node{
			Id:       id,
			Name:     n.Attributes.Name,
			Path:     path,
			URL:      n.Links.Download,
			IsDir:    n.Attributes.Kind != "file",
			Hash:     hash,
			HashType: hashType,
			Size:     n.Attributes.Size,
		})
	}
	return nodes, nil
}

/*

"children":
"data":

[

{

    "id": "2vewn",
    "type": "nodes",
    "attributes":

{

    "title": "A Good Node Title",
{

    "links":

{

    "related":

        {
            "href": "https://api.osf.io/v2/nodes/2vewn/children/",
            "meta": { }
        }
    }

},
"comments":

{},
"contributors":

{},
"bibliographic_contributors":

{},
"implicit_contributors":

{},
"files":

{

    "links":

{

    "related":

        {
            "href": "https://api.osf.io/v2/nodes/2vewn/files/",
            "meta": { }
        }
    }

},
*/
