package gitlab

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"net/url"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	base := streamParams.Url
	group := streamParams.User
	project := streamParams.RepoName
	token := streamParams.Token
	if project == "" || token == "" || base == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected base, group (optional), project and token, got: %v", streamParams)
	}
	res := map[string]types.Stream{}

	for k, v := range in {
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return nil, fmt.Errorf("streams: sha not found")
		}
		sep := "/"
		if group == "" {
			sep = ""
		}
		url := base + "/api/v4/projects/" + url.PathEscape(group+sep+project) + "/repository/blobs/" + sha + "/raw"
		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Add("PRIVATE-TOKEN", token)
		var r *http.Response

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				r, err = http.DefaultClient.Do(request)
				if err != nil {
					return nil, err
				}
				if r.StatusCode != 200 {
					b, _ := io.ReadAll(r.Body)
					return nil, fmt.Errorf("getting file failed: %s", string(b))
				}
				return r.Body, nil
			},
			Close: func() error {
				return r.Body.Close()
			},
		}
	}
	return res, nil
}
