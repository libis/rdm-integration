// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package sftp_plugin

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	if streamParams.Url == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: expected url")
	}
	res := map[string]types.Stream{}

	c, err := getClient(streamParams.Url, streamParams.User, streamParams.Token)
	if err != nil {
		return types.StreamsType{}, fmt.Errorf("streams: failed to open sftp client: %v", err)
	}

	// Do something with the FTP conn

	for k, v := range in {
		var err error
		var reader io.ReadCloser
		id := v.Id

		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				reader, err = c.SftpClient.Open(streamParams.Option + id)

				return reader, err
			},
			Close: func() error {
				return reader.Close()
			},
		}
	}
	return types.StreamsType{Streams: res, Cleanup: func() error {
		return c.Close()
	}}, nil
}
