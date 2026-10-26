// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

// Package plugintest installs a recording fake plugin in the registry, so a
// test can see whether code handed a request to a plugin.
package plugintest

import (
	"context"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sync"
	"testing"
)

// Node is the only file the fake's Query reports.
var Node = tree.Node{Id: "fake.txt", Name: "fake.txt", Attributes: tree.Attributes{IsFile: true, RemoteHash: "abc"}}

// Item is the only item the fake's Options and Search return.
var Item = types.SelectItem{Label: "fake item", Value: "fake item"}

// Title is the dataset title the fake's Metadata returns.
const Title = "fake title"

// Recorder is a fake plugin that counts the calls to its functions.
type Recorder struct {
	mu    sync.Mutex
	calls int
}

// Install registers a new Recorder under name, replacing any plugin of that
// name for the rest of the test binary, and makes only the plugin types in
// configured available until the test ends.
func Install(t testing.TB, name string, configured ...string) *Recorder {
	t.Helper()
	r := &Recorder{}
	plugin.Register(name, r.plugin())
	plugin.Configure(configured)
	t.Cleanup(func() { plugin.Configure(nil) })
	return r
}

// Calls returns how many times any function of the fake ran.
func (r *Recorder) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *Recorder) record() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
}

func (r *Recorder) plugin() plugin.Plugin {
	return plugin.Plugin{
		Query: func(context.Context, types.CompareRequest, map[string]tree.Node) (map[string]tree.Node, error) {
			r.record()
			return map[string]tree.Node{Node.Id: Node}, nil
		},
		Options: func(context.Context, types.OptionsRequest) ([]types.SelectItem, error) {
			r.record()
			return []types.SelectItem{Item}, nil
		},
		Search: func(context.Context, types.OptionsRequest) ([]types.SelectItem, error) {
			r.record()
			return []types.SelectItem{Item}, nil
		},
		Streams: func(context.Context, map[string]tree.Node, types.StreamParams) (types.StreamsType, error) {
			r.record()
			return types.StreamsType{Streams: map[string]types.Stream{}}, nil
		},
		Metadata: func(context.Context, types.StreamParams) (types.MetadataStruct, error) {
			r.record()
			return types.MetadataStruct{Title: Title}, nil
		},
	}
}
