// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"sync"
)

var Destination DestinationPlugin

type DestinationPlugin struct {
	IsDirectUpload        func() bool
	CheckPermission       func(ctx context.Context, token, user, persistentId string) error
	CreateNewRepo         func(ctx context.Context, collection, token, userName string) (string, error)
	GetRepoUrl            func(pid string, draft bool) string
	WriteOverWire         func(ctx context.Context, dbId int64, nodeMapId, token, user, persistentId string, wg *sync.WaitGroup, async_err *ErrorHolder) (io.WriteCloser, error)
	SaveAfterDirectUpload func(ctx context.Context, replace bool, token, user, persistentId string, storageIdentifiers []string, nodes []tree.Node) error
	CleanupLeftOverFiles  func(ctx context.Context, persistentId, token, user string) error
	DeleteFile            func(ctx context.Context, token, user string, id int64) error
	Options               func(ctx context.Context, objectType, collection, searchTerm, token, user string) ([]types.SelectItem, error)
	GetStream             func(ctx context.Context, token, user string, id int64) (io.ReadCloser, error)
	Query                 func(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, error)
	GetUserEmail          func(ctx context.Context, token, user string) (string, error)
}
