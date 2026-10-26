// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package main

import (
	"integration/app/frontend"
	"integration/app/plugin"
	"testing"
)

func TestLocalBinaryServesTheLocalPlugin(t *testing.T) {
	saved := frontend.Config.Plugins
	t.Cleanup(func() {
		frontend.Config.Plugins = saved
		plugin.Configure(nil)
	})
	configurePlugins()
	if p, err := plugin.GetPlugin("local"); err != nil || p.Query == nil || p.Streams == nil {
		t.Fatalf("the local binary does not serve the local plugin: %v", err)
	}
	if first := frontend.Config.Plugins[0]; first.Id != "local" || first.Plugin != "local" {
		t.Errorf("the local plugin is not offered first: %+v", first)
	}
	for _, p := range saved {
		if _, err := plugin.GetPlugin(p.Plugin); err != nil {
			t.Errorf("configured plugin %s is not available: %v", p.Plugin, err)
		}
	}
}
