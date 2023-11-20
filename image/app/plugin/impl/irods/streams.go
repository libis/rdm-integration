// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error) {
	user := streamParams.User
	password := streamParams.Token
	server := streamParams.Url
	zone := streamParams.RepoName
	folder := streamParams.Option
	if user == "" || password == "" || server == "" || zone == "" || folder == "" {
		return nil, fmt.Errorf("folders: missing parameters: expected server, zone, folder, user and password, got: %+v", streamParams)
	}
	res := map[string]types.Stream{}
	for k, v := range in {
		path := v.Id
		if !v.Attributes.IsFile || (v.Action != tree.Update && v.Action != tree.Copy) {
			continue
		}
		if path == "" {
			return nil, fmt.Errorf("streams: path not found")
		}

		var cl *IrodsClient
		var reader io.ReadCloser
		res[k] = types.Stream{
			Open: func() (io.Reader, error) {
				var irodsErr error
				cl, irodsErr = NewIrodsClient(server, zone, user, password)
				if irodsErr != nil {
					return nil, irodsErr
				}
				reader, irodsErr = cl.StreamFile(folder + "/" + path)
				return reader, irodsErr
			},
			Close: func() error {
				if reader == nil {
					return fmt.Errorf("iRods reader is nil, close not possible")
				}
				closeErr := reader.Close()
				cl.Close()
				if cl == nil {
					return fmt.Errorf("iRods client is nil, close not possible")
				}
				return closeErr
			},
		}
	}
	return res, nil
}
