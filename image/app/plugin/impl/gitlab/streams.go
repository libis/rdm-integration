// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

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

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	base := streamParams.Url
	project := streamParams.RepoName
	token := streamParams.Token
	if project == "" || token == "" || base == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: expected base, group (optional), project and token")
	}
	res := map[string]types.Stream{}

	for k, v := range in {
		sha := v.Attributes.RemoteHash
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if sha == "" {
			return types.StreamsType{}, fmt.Errorf("streams: sha not found")
		}
		url := base + "/api/v4/projects/" + url.PathEscape(project) + "/repository/blobs/" + sha + "/raw"
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return types.StreamsType{}, err
		}
		request.Header.Add("Authorization", "Bearer "+token)
		var r *http.Response

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				r, err = http.DefaultClient.Do(request)
				if err != nil {
					return nil, err
				}
				if r.StatusCode != 200 {
					b, _ := io.ReadAll(r.Body)
					r.Body.Close()
					return nil, fmt.Errorf("getting file failed: %s", string(b))
				}
				return r.Body, nil
			},
			Close: func() error {
				return r.Body.Close()
			},
		}
	}
	return types.StreamsType{Streams: res, Cleanup: nil}, nil
}
