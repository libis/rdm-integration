// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package stream

import (
	"context"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
)

// Streams opens the streams of a job's plugin. A job whose plugin the
// configuration does not enable fails without retries.
func Streams(ctx context.Context, nodeMap map[string]tree.Node, pluginName string, streamParams types.StreamParams) (types.StreamsType, error) {
	p, err := plugin.GetPlugin(pluginName)
	if err != nil {
		return types.StreamsType{}, types.NewUnrecoverableError(err)
	}
	return p.Streams(ctx, nodeMap, streamParams)
}
