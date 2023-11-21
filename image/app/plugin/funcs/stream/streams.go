// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package stream

import (
	"context"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
)

func Streams(ctx context.Context, nodeMap map[string]tree.Node, pluginName string, streamParams types.StreamParams) (types.StreamsType, error) {
	return plugin.GetPlugin(pluginName).Streams(ctx, nodeMap, streamParams)
}
