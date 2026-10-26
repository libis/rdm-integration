// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package osf

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	token := streamParams.Token
	if token == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: expected token")
	}
	res := map[string]types.Stream{}

	for k, v := range in {
		request, err := http.NewRequestWithContext(ctx, "GET", v.Attributes.URL, nil)
		if err != nil {
			return types.StreamsType{}, fmt.Errorf("OSF download request could not be created")
		}
		request.Header.Add("Authorization", "Bearer "+token)
		var r *http.Response

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				r, err = http.DefaultClient.Do(request)
				if err != nil {
					return nil, fmt.Errorf("OSF file download failed")
				}
				if r.StatusCode != 200 {
					r.Body.Close()
					return nil, fmt.Errorf("OSF file download failed")
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
