package stream

import (
	"context"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
)

func Streams(ctx context.Context, nodeMap map[string]tree.Node, streamType string, streamParams types.StreamParams) (map[string]types.Stream, error) {
	return plugin.GetPlugin(streamType).Streams(ctx, nodeMap, streamParams)
}
