// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package local

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"os"
	"strings"
)

func Streams(_ context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	url := streamParams.Url
	if url == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, got: %v", streamParams)
	}
	if strings.HasSuffix(url, string(os.PathSeparator)) {
		url = url[:len(url)-1]
	}
	res := map[string]types.Stream{}
	for k, v := range in {
		var err error
		var reader io.ReadCloser
		id := v.Id

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				reader, err = os.Open(url + string(os.PathSeparator) + id)
				return reader, err
			},
			Close: func() error {
				return reader.Close()
			},
		}
	}
	return res, nil
}
