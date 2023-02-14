// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	token := streamParams.Token
	url := fmt.Sprintf("%s/api/", streamParams.Url)
	if token == "" || url == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %v", streamParams)
	}
	res := map[string]types.Stream{}

	for k, v := range in {
		data, _ := json.Marshal(Request{
			Token:        token,
			Content:      "fileRepository",
			Action:       "export",
			DocId:        v.Attributes.URL,
			ReturnFormat: "json",
		})
		request, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
		if err != nil {
			return nil, err
		}
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
	return res, nil
}
