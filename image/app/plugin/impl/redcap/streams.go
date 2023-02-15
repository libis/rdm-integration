// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package redcap

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"strconv"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	token := streamParams.Token
	url := fmt.Sprintf("%s/api/", streamParams.Url)
	if token == "" || url == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, token, got: %v", streamParams)
	}
	res := map[string]types.Stream{}

	for k, v := range in {
		docId, _ := strconv.Atoi(v.Attributes.URL)
		data := Request{
			Token:        token,
			Content:      "fileRepository",
			Action:       "export",
			DocId:        int64(docId),
			ReturnFormat: "json",
		}
		request, err := http.NewRequestWithContext(ctx, "POST", url, encode(data))
		if err != nil {
			return nil, err
		}
		request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Add("Accept", "application/json")
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
