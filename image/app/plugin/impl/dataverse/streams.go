// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"github.com/libis/rdm-dataverse-go-api/api"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	res := map[string]types.Stream{}
	client, _ := newClient(streamParams.PluginId, streamParams.Url, streamParams.User, streamParams.Token)
	for k, v := range in {
		var reader io.ReadCloser
		var err error
		req := client.NewRequest(v.Attributes.URL, "GET", nil, nil)
		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				reader, err = api.DoStream(ctx, req)
				return reader, err
			},
			Close: func() error {
				return reader.Close()
			},
		}
	}
	return res, nil
}
