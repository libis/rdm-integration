// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	user := streamParams.User
	password := streamParams.Token
	server := streamParams.Url
	zone := streamParams.RepoName
	folder := streamParams.Option
	if user == "" || password == "" || server == "" || zone == "" || folder == "" {
		return types.StreamsType{}, fmt.Errorf("folders: missing parameters: expected server, zone, folder, user and password, got: %+v", streamParams)
	}
	cl, clientErr := NewIrodsClient(server, zone, user, password)
	if clientErr != nil {
		return types.StreamsType{}, clientErr
	}
	res := map[string]types.Stream{}
	for k, v := range in {
		path := v.Id
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if path == "" {
			return types.StreamsType{}, fmt.Errorf("streams: path not found")
		}

		var reader io.ReadCloser
		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				var err error
				reader, err = cl.StreamFile(folder + "/" + path)
				return reader, err
			},
			Close: func() (err error) {
				if reader == nil {
					err = fmt.Errorf("iRods reader is nil, close not possible")
				} else {
					err = reader.Close()
				}
				return
			},
		}
	}
	return types.StreamsType{Streams: res, Cleanup: cl.Close}, nil
}
