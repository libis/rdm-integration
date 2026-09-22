// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"integration/app/tree"
	"testing"
)

const testPid = "doi:10.1/GLOBUS"

func globusNode(id, timestamp string) tree.Node {
	return tree.Node{
		Id:   id,
		Name: id,
		Attributes: tree.Attributes{
			IsFile:         true,
			RemoteHash:     timestamp,
			RemoteHashType: types.LastModified,
			DestinationFile: tree.DestinationFile{
				Id:       42,
				Hash:     "md5-from-dataverse",
				HashType: types.Md5,
			},
		},
	}
}

func TestCalculateHashUsesRecordedGlobusTimestampOnce(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	config.GetRedis().Set(ctx, globusTransferKey(testPid, n.Id), n.Attributes.RemoteHash, 0)

	known := map[string]calculatedHashes{}
	if err := calculateHash(ctx, "", "", testPid, n, known); err != nil {
		t.Fatal(err)
	}
	if got := known[n.Id].RemoteHashes[types.LastModified]; got != n.Attributes.RemoteHash {
		t.Errorf("expected the recorded timestamp as remote hash, got %q", got)
	}
	if known[n.Id].LocalHashValue != "md5-from-dataverse" || known[n.Id].LocalHashType != types.Md5 {
		t.Errorf("expected the cache entry to be keyed to the destination hash, got %+v", known[n.Id])
	}
	if v := config.GetRedis().Get(ctx, globusTransferKey(testPid, n.Id)).Val(); v != "" {
		t.Error("expected the timestamp marker to be consumed")
	}
	value, needsJob := resolveDestinationHash(n, known[n.Id], "")
	if value != n.Attributes.RemoteHash || needsJob {
		t.Errorf("expected the file to resolve equal without a job, got %q needsJob=%v", value, needsJob)
	}
}

func TestCalculateHashMarkerOverridesStaleUnknownForSameContent(t *testing.T) {
	// The file was transferred before, hashed as "unknown", then deleted and
	// transferred again with identical content: same destination checksum,
	// so the cache entry still matches. The new transfer's timestamp must win.
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	n := globusNode("README.md", "2025-12-19 11:46:42+00:00")
	known := map[string]calculatedHashes{n.Id: {
		LocalHashType:  types.Md5,
		LocalHashValue: "md5-from-dataverse",
		RemoteHashes:   map[string]string{types.LastModified: fmt.Sprintf("%x", "unknown")},
	}}
	config.GetRedis().Set(ctx, globusTransferKey(testPid, n.Id), n.Attributes.RemoteHash, 0)
	if err := calculateHash(ctx, "", "", testPid, n, known); err != nil {
		t.Fatal(err)
	}
	if got := known[n.Id].RemoteHashes[types.LastModified]; got != n.Attributes.RemoteHash {
		t.Errorf("expected the new transfer's timestamp to replace the stale unknown, got %q", got)
	}
}

func TestCalculateHashKeepsCachedHashOfOtherTypes(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	n.Attributes.RemoteHashType = types.SHA256
	n.Attributes.RemoteHash = "abc"
	known := map[string]calculatedHashes{n.Id: {
		LocalHashType:  types.Md5,
		LocalHashValue: "md5-from-dataverse",
		RemoteHashes:   map[string]string{types.SHA256: "abc"},
	}}
	if err := calculateHash(context.Background(), "", "", testPid, n, known); err != nil {
		t.Fatal(err)
	}
	if known[n.Id].RemoteHashes[types.SHA256] != "abc" {
		t.Error("expected the cached hash to be kept without recomputing")
	}
}

func TestCalculateHashWithoutRecordedTimestampStaysUnknown(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	known := map[string]calculatedHashes{}
	if err := calculateHash(context.Background(), "", "", testPid, n, known); err != nil {
		t.Fatal(err)
	}
	if got := known[n.Id].RemoteHashes[types.LastModified]; got != fmt.Sprintf("%x", "unknown") {
		t.Errorf("expected the unknown marker, got %q", got)
	}
}

func TestGlobusPersistRecordsSourceTimestamps(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		CheckPermission: func(context.Context, string, string, string) error { return nil },
	}
	ctx := context.Background()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	n.Action = tree.Copy
	deleted := globusNode("gone.bin", "2026-01-01 00:00:00+00:00")
	deleted.Action = tree.Delete
	deleted.Attributes.DestinationFile.Id = 0
	job := Job{PersistentId: testPid, Plugin: "globus", WritableNodes: map[string]tree.Node{n.Id: n, deleted.Id: deleted}}
	Destination.DeleteFiles = func(context.Context, string, string, string, []int64) error { return nil }

	out, err := doPersistNodeMap(ctx, nil, job, map[string]calculatedHashes{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.WritableNodes) != 0 {
		t.Errorf("expected the globus job to hand every node to the transfer, got %v", out.WritableNodes)
	}
	if v := config.GetRedis().Get(ctx, globusTransferKey(testPid, n.Id)).Val(); v != n.Attributes.RemoteHash {
		t.Errorf("expected the source timestamp to be recorded for the transferred file, got %q", v)
	}
	if v := config.GetRedis().Get(ctx, globusTransferKey(testPid, deleted.Id)).Val(); v != "" {
		t.Errorf("expected no timestamp for a deleted file, got %q", v)
	}
}

func TestPolledCompareRefreshesDestinationBeforeQueuing(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	fresh := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	queries := 0
	Destination = DestinationPlugin{
		GetRepoUrl: func(string, bool) string { return "" },
		Query: func(context.Context, string, string, string) (map[string]tree.Node, error) {
			queries++
			return map[string]tree.Node{fresh.Id: fresh}, nil
		},
	}
	ctx := context.Background()
	// The page polls with the node it was shown: the destination hash is "?".
	echoed := fresh
	echoed.Attributes.DestinationFile.Hash = "?"
	nodes := map[string]tree.Node{echoed.Id: echoed}

	res := Compare(ctx, nodes, testPid, "key", "user", false)
	if res.Status != Updating {
		t.Errorf("expected Updating while the hash job runs, got %v", res.Status)
	}
	job, ok := popJob("")
	if !ok || job.Plugin != "hash-only" {
		t.Fatalf("expected the polled compare to queue the hash job, got ok=%v job=%+v", ok, job)
	}
	if got := job.WritableNodes[fresh.Id].Attributes.DestinationFile.Hash; got != "md5-from-dataverse" {
		t.Errorf("expected the job to carry the refreshed destination hash, got %q", got)
	}
	if queries != 1 || !IsLocked(ctx, testPid) {
		t.Errorf("expected one destination query and the lock held, got queries=%d locked=%v", queries, IsLocked(ctx, testPid))
	}

	// A second poll while the job holds the lock neither queries nor queues.
	res = Compare(ctx, nodes, testPid, "key", "user", false)
	if res.Status != Updating {
		t.Errorf("expected Updating while locked, got %v", res.Status)
	}
	if _, ok := popJob(""); ok || queries != 1 {
		t.Errorf("expected no second job or query while locked, got job=%v queries=%d", ok, queries)
	}
}

func TestPolledCompareStaysUpdatingWhenRefreshFails(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		GetRepoUrl: func(string, bool) string { return "" },
		Query: func(context.Context, string, string, string) (map[string]tree.Node, error) {
			return nil, fmt.Errorf("dataverse down")
		},
	}
	echoed := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	echoed.Attributes.DestinationFile.Hash = "?"
	res := Compare(context.Background(), map[string]tree.Node{echoed.Id: echoed}, testPid, "key", "user", false)
	if res.Status != Updating {
		t.Errorf("expected Updating, got %v", res.Status)
	}
	if _, ok := popJob(""); ok {
		t.Error("expected no job built from an echoed unknown hash")
	}
}

func TestRehashJobNeverContainsEchoedUnknownHashes(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	echoed := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	echoed.Attributes.DestinationFile.Hash = "?"
	_, jobNeeded := localRehashToMatchRemoteHashType(context.Background(), "key", "user", testPid, map[string]tree.Node{echoed.Id: echoed}, true)
	if !jobNeeded {
		t.Error("expected the status to keep reporting a needed job")
	}
	if _, ok := popJob(""); ok {
		t.Error("expected no job for a node without a real destination hash")
	}
}
