// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package plugin

import (
	"context"
	"integration/app/plugin/impl/dataverse"
	"integration/app/plugin/impl/github"
	"integration/app/plugin/impl/gitlab"
	"integration/app/plugin/impl/globus"
	"integration/app/plugin/impl/irods"
	"integration/app/plugin/impl/local"
	"integration/app/plugin/impl/onedrive"
	"integration/app/plugin/impl/osf"
	"integration/app/plugin/impl/redcap"
	"integration/app/plugin/impl/sftp_plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
)

type Plugin struct {
	Query   func(ctx context.Context, req types.CompareRequest, dvNodes map[string]tree.Node) (map[string]tree.Node, error)
	Options func(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error)
	Search  func(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error)
	Streams func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error)
}

var pluginMap map[string]Plugin = map[string]Plugin{
	"github": {
		Query:   github.Query,
		Options: github.Options,
		Search:  github.Search,
		Streams: github.Streams,
	},
	"gitlab": {
		Query:   gitlab.Query,
		Options: gitlab.Options,
		Search:  gitlab.Search,
		Streams: gitlab.Streams,
	},
	"irods": {
		Query:   irods.Query,
		Options: irods.Options,
		Search:  irods.Search,
		Streams: irods.Streams,
	},
	"redcap": {
		Query:   redcap.Query,
		Options: redcap.Options,
		Search:  nil,
		Streams: redcap.Streams,
	},
	"osf": {
		Query:   osf.Query,
		Options: nil,
		Search:  osf.Search,
		Streams: osf.Streams,
	},
	"onedrive": {
		Query:   onedrive.Query,
		Options: onedrive.Options,
		Search:  onedrive.Search,
		Streams: onedrive.Streams,
	},
	"dataverse": {
		Query:   dataverse.Query,
		Options: nil,
		Search:  dataverse.Search,
		Streams: dataverse.Streams,
	},
	"local": {
		Query:   local.Query,
		Options: nil,
		Search:  nil,
		Streams: local.Streams,
	},
	"sftp": {
		Query:   sftp_plugin.Query,
		Options: sftp_plugin.Options,
		Search:  nil,
		Streams: sftp_plugin.Streams,
	},
	"globus": {
		Query:   globus.Query,
		Options: globus.Options,
		Search:  globus.Search,
		Streams: globus.Streams,
	},
}

func GetPlugin(p string) Plugin {
	return pluginMap[p]
}
