// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

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
	Query   func(req types.CompareRequest, dvNodes map[string]tree.Node) (map[string]tree.Node, error)
	Options func(params types.OptionsRequest) ([]string, error)
	Streams func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (map[string]types.Stream, error)
	Name    string
}

var pluginMap map[string]Plugin = map[string]Plugin{
	"github": {
		Query:   github.Query,
		Options: github.Options,
		Streams: github.Streams,
		Name:    "GitHub",
	},
	"gitlab": {
		Query:   gitlab.Query,
		Options: gitlab.Options,
		Streams: gitlab.Streams,
		Name:    "GitLab",
	},
	"irods": {
		Query:   irods.Query,
		Options: irods.Options,
		Streams: irods.Streams,
		Name:    "IRODS",
	},
	"local": {
		Query:   local.Query,
		Options: nil,
		Streams: local.Streams,
		Name:    "Local filesystem",
	},
}

func GetPluginToNameMap() map[string]string {
	res := map[string]string{}
	for k, v := range pluginMap {
		res[k] = v.Name
	}
	return res
}

func GetPlugin(p string) Plugin {
	return pluginMap[p]
}
