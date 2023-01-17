package local

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"os"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	url := streamParams.Url
	if url == "" {
		return nil, fmt.Errorf("streams: missing parameters: expected url, got: %v", streamParams)
	}
	res := map[string]types.Stream{}
	for k, v := range in {
		var err error
		var reader io.ReadCloser

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				reader, err = os.Open(url + string(os.PathSeparator) + v.Id)
				return reader, err
			},
			Close: func() error {
				return reader.Close()
			},
		}
	}
	return res, nil
}
