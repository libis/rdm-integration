package plugin

import (
	"context"
	"integration/app/plugin/impl/github"
	"integration/app/plugin/impl/gitlab"
	"integration/app/plugin/impl/irods"
	"integration/app/plugin/impl/local"
	"integration/app/plugin/types"
	"integration/app/tree"
)

type Plugin struct {
	Query   func(req types.CompareRequest) (map[string]tree.Node, error)
	Options func(params types.OptionsRequest) ([]string, error)
	Streams func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error)
}

var pluginMap map[string]Plugin = map[string]Plugin{
	"github": {
		Query:   github.Query,
		Options: github.Options,
		Streams: github.Streams,
	},
	"gitlab": {
		Query:   gitlab.Query,
		Options: gitlab.Options,
		Streams: gitlab.Streams,
	},
	"irods": {
		Query:   irods.Query,
		Options: irods.Options,
		Streams: irods.Streams,
	},
	"local": {
		Query:   local.Query,
		Options: nil,
		Streams: local.Streams,
	},
}

func GetPlugin(p string) Plugin {
	return pluginMap[p]
}
