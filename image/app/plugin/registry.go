// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package plugin

import (
	"context"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/impl/dataverse"
	"integration/app/plugin/impl/github"
	"integration/app/plugin/impl/gitlab"
	"integration/app/plugin/impl/globus"
	"integration/app/plugin/impl/irods"
	"integration/app/plugin/impl/local"
	"integration/app/plugin/impl/onedrive"
	"integration/app/plugin/impl/osf"
	"integration/app/plugin/impl/redcap"
	"integration/app/plugin/impl/redcap2"
	"integration/app/plugin/impl/sftp_plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sync"
)

type Plugin struct {
	Query    func(ctx context.Context, req types.CompareRequest, dvNodes map[string]tree.Node) (map[string]tree.Node, error)
	Options  func(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error)
	Search   func(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error)
	Streams  func(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error)
	Metadata func(ctx context.Context, streamParams types.StreamParams) (types.MetadataStruct, error)
}

// pluginMap holds every plugin built into this binary. Requests and jobs only
// reach the ones that Configure keeps.
var pluginMap map[string]Plugin = map[string]Plugin{
	"github": {
		Query:    github.Query,
		Options:  github.Options,
		Search:   github.Search,
		Streams:  github.Streams,
		Metadata: github.Metadata,
	},
	"gitlab": {
		Query:    gitlab.Query,
		Options:  gitlab.Options,
		Search:   gitlab.Search,
		Streams:  gitlab.Streams,
		Metadata: gitlab.Metadata,
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
	"redcap2": {
		Query:    redcap2.Query,
		Options:  redcap2.Options,
		Search:   nil,
		Streams:  redcap2.Streams,
		Metadata: redcap2.Metadata,
	},
	"osf": {
		Query:    osf.Query,
		Options:  nil,
		Search:   osf.Search,
		Streams:  osf.Streams,
		Metadata: osf.Metadata,
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

var (
	mu        sync.RWMutex
	available = map[string]Plugin{}
)

// Configure filters the registry: it keeps the built-in plugins whose types
// are listed and drops every other plugin. Each binary calls it once its
// configuration is loaded; until then no plugin is available.
func Configure(pluginTypes []string) {
	mu.Lock()
	defer mu.Unlock()
	available = map[string]Plugin{}
	kept := []string{}
	for _, t := range pluginTypes {
		p, ok := pluginMap[t]
		if !ok {
			logging.Logger.Printf("configured plugin type %q is not built in, ignoring it\n", t)
			continue
		}
		if _, dup := available[t]; !dup {
			available[t] = p
			kept = append(kept, t)
		}
	}
	logging.Logger.Printf("available plugins: %v\n", kept)
}

// Register adds p to the built-in plugins under the type name, replacing any
// plugin of that name. Configure still decides whether it is available; tests
// use this to install fakes.
func Register(name string, p Plugin) {
	mu.Lock()
	defer mu.Unlock()
	pluginMap[name] = p
}

// GetPlugin returns the plugin of the given type when the configuration
// enables it, and an error naming the type otherwise.
func GetPlugin(name string) (Plugin, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := available[name]
	if !ok {
		return Plugin{}, fmt.Errorf("plugin is not configured")
	}
	return p, nil
}
