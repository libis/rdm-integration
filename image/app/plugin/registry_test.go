// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package plugin_test

import (
	"integration/app/plugin"
	"strings"
	"testing"
)

var builtIn = []string{"github", "gitlab", "irods", "redcap", "redcap2", "osf", "onedrive", "dataverse", "local", "sftp", "globus"}

// Keep this test first: it checks the state before any Configure call.
func TestNoPluginIsAvailableUntilConfigured(t *testing.T) {
	for _, name := range builtIn {
		if _, err := plugin.GetPlugin(name); err == nil {
			t.Errorf("%s is available although nothing is configured", name)
		}
	}
}

func TestOnlyConfiguredPluginsAreAvailable(t *testing.T) {
	plugin.Configure([]string{"github", "sftp"})
	t.Cleanup(func() { plugin.Configure(nil) })
	for _, name := range []string{"github", "sftp"} {
		if p, err := plugin.GetPlugin(name); err != nil || p.Query == nil || p.Streams == nil {
			t.Errorf("configured plugin %s is not available: %v", name, err)
		}
	}
	_, err := plugin.GetPlugin("local")
	if err == nil || !strings.Contains(err.Error(), `"local"`) {
		t.Errorf("expected an error that names the unconfigured plugin, got %v", err)
	}
}

func TestConfiguringAnUnregisteredTypeEnablesNothing(t *testing.T) {
	plugin.Configure([]string{"no-such-plugin"})
	t.Cleanup(func() { plugin.Configure(nil) })
	if _, err := plugin.GetPlugin("no-such-plugin"); err == nil {
		t.Error("an unregistered plugin type became available")
	}
}

func TestConfigureReplacesTheAvailablePlugins(t *testing.T) {
	plugin.Configure([]string{"github"})
	plugin.Configure([]string{"gitlab"})
	t.Cleanup(func() { plugin.Configure(nil) })
	if _, err := plugin.GetPlugin("github"); err == nil {
		t.Error("a plugin stayed available after the configuration dropped it")
	}
	if _, err := plugin.GetPlugin("gitlab"); err != nil {
		t.Errorf("gitlab is not available: %v", err)
	}
}
