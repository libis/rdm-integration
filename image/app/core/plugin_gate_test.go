// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"context"
	"errors"
	"integration/app/config"
	"integration/app/plugin/plugintest"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"integration/app/tree"
	"strings"
	"testing"
	"time"
)

var errStopBeforeWriting = errors.New("stop before writing")

// stopAtPermissionCheck ends a job at its first destination call, the
// permission check, so a test sees how far the job got without a Dataverse.
func stopAtPermissionCheck(t *testing.T) {
	t.Helper()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	saved := Destination
	Destination.CheckPermission = func(context.Context, string, string, string) error {
		return errStopBeforeWriting
	}
	t.Cleanup(func() {
		Destination = saved
		fr.Reset()
	})
}

func jobFor(pluginName string) Job {
	return Job{
		PersistentId:  "doi:10.5072/FK2/JOB",
		Plugin:        pluginName,
		WritableNodes: map[string]tree.Node{"etc/passwd": {Id: "etc/passwd", Name: "passwd", Attributes: tree.Attributes{IsFile: true}}},
		StreamParams:  types.StreamParams{PluginId: pluginName, Url: "/"},
		Deadline:      time.Now().Add(time.Minute),
	}
}

func TestJobWithAnUnconfiguredPluginFailsWithoutRetry(t *testing.T) {
	stopAtPermissionCheck(t)
	fake := plugintest.Install(t, "local", "github", "gitlab")
	_, err := DoWork(jobFor("local"))
	if !types.IsUnrecoverable(err) || !strings.Contains(err.Error(), `"local"`) {
		t.Errorf("expected an unrecoverable error that names the plugin, got %v", err)
	}
	if fake.Calls() != 0 {
		t.Errorf("the unconfigured plugin ran %d times", fake.Calls())
	}
}

func TestJobStreamsFromAConfiguredPlugin(t *testing.T) {
	stopAtPermissionCheck(t)
	fake := plugintest.Install(t, "fake", "fake")
	_, err := DoWork(jobFor("fake"))
	if !errors.Is(err, errStopBeforeWriting) || fake.Calls() != 1 {
		t.Errorf("configured plugin not streamed: %d calls, err %v", fake.Calls(), err)
	}
}

// A rehash job is not a plugin job, so it runs with no plugin configured.
func TestHashOnlyJobRunsWithNoPluginConfigured(t *testing.T) {
	stopAtPermissionCheck(t)
	plugintest.Install(t, "fake")
	_, err := DoWork(jobFor("hash-only"))
	if !errors.Is(err, errStopBeforeWriting) {
		t.Errorf("rehash job did not reach its permission check: %v", err)
	}
}
