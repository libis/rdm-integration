// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"integration/app/tree"
	"testing"
)

const testPid = "doi:10.1/GLOBUS"
const testStorageObject = "s3://dataverse-pilot:19a2b3c4d5e-0123456789ab"

func recordTransfer(t *testing.T, nodeId, storageIdentifier, lastModified string) {
	t.Helper()
	b, err := json.Marshal(types.GlobusTransfer{StorageIdentifier: storageIdentifier, LastModified: lastModified})
	if err != nil {
		t.Fatal(err)
	}
	config.GetRedis().Set(context.Background(), types.GlobusTransferKey(testPid, nodeId), string(b), 0)
}

func globusNode(id, timestamp string) tree.Node {
	return tree.Node{
		Id:   id,
		Name: id,
		Attributes: tree.Attributes{
			IsFile:         true,
			RemoteHash:     timestamp,
			RemoteHashType: types.LastModified,
			DestinationFile: tree.DestinationFile{
				Id:                42,
				Hash:              "md5-from-dataverse",
				HashType:          types.Md5,
				StorageIdentifier: testStorageObject,
			},
		},
	}
}

func TestCalculateHashUsesRecordedGlobusTimestampForTheTransferredObject(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	recordTransfer(t, n.Id, testStorageObject, n.Attributes.RemoteHash)

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
	if v := config.GetRedis().Get(ctx, types.GlobusTransferKey(testPid, n.Id)).Val(); v == "" {
		t.Error("expected the transfer record to be kept for a rebuild of a wiped cache")
	}
	value, needsJob := resolveDestinationHash(n, known[n.Id], "")
	if value != n.Attributes.RemoteHash || needsJob {
		t.Errorf("expected the file to resolve equal without a job, got %q needsJob=%v", value, needsJob)
	}
}

func TestCalculateHashIgnoresRecordOfAnotherStorageObject(t *testing.T) {
	// The transfer never delivered (or the file was replaced since): the
	// object in Dataverse is not the one the record describes.
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	recordTransfer(t, n.Id, "s3://dataverse-pilot:19a2b3c4d5e-ffffffffffff", n.Attributes.RemoteHash)

	known := map[string]calculatedHashes{}
	if err := calculateHash(ctx, "", "", testPid, n, known); err != nil {
		t.Fatal(err)
	}
	if got := known[n.Id].RemoteHashes[types.LastModified]; got != fmt.Sprintf("%x", "unknown") {
		t.Errorf("expected unrelated content to stay unknown, got %q", got)
	}
	if v := config.GetRedis().Get(ctx, types.GlobusTransferKey(testPid, n.Id)).Val(); v == "" {
		t.Error("expected the record to be kept until its object arrives or it expires")
	}
	value, needsJob := resolveDestinationHash(n, known[n.Id], "")
	if value == n.Attributes.RemoteHash || needsJob {
		t.Errorf("expected the file to show as updated, got %q needsJob=%v", value, needsJob)
	}
}

func TestCalculateHashIgnoresRecordsWithoutStorageObject(t *testing.T) {
	// Records written before the object binding hold a bare timestamp.
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	n := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	for name, raw := range map[string]string{
		"bare timestamp": n.Attributes.RemoteHash,
		"empty object":   `{"storageIdentifier":"","lastModified":"` + n.Attributes.RemoteHash + `"}`,
	} {
		config.GetRedis().Set(ctx, types.GlobusTransferKey(testPid, n.Id), raw, 0)
		known := map[string]calculatedHashes{}
		if err := calculateHash(ctx, "", "", testPid, n, known); err != nil {
			t.Fatal(err)
		}
		if got := known[n.Id].RemoteHashes[types.LastModified]; got != fmt.Sprintf("%x", "unknown") {
			t.Errorf("%s: expected the record to be ignored, got %q", name, got)
		}
	}
}

func TestSameStorageObject(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"s3://dataverse-pilot:19a2b3c4d5e-01", "s3://dataverse-pilot:19a2b3c4d5e-01", true},
		{"s3://dataverse-pilot:19a2b3c4d5e-01", "19a2b3c4d5e-01", true},
		{"s3://dataverse-pilot/19a2b3c4d5e-01", "s3://dataverse-pilot:19a2b3c4d5e-01", true},
		{"s3://dataverse-pilot:19a2b3c4d5e-01", "s3://dataverse-pilot:19a2b3c4d5e-02", false},
		{"s3://dataverse-pilot:19a2b3c4d5e-01", "", false},
		{"", "", false},
		{"s3://dataverse-pilot:", "s3://dataverse-pilot:", false},
	}
	for _, tt := range tests {
		if got := sameStorageObject(tt.a, tt.b); got != tt.want {
			t.Errorf("sameStorageObject(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
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
	recordTransfer(t, n.Id, testStorageObject, n.Attributes.RemoteHash)
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

func TestGlobusPersistDropsCachedHashesOfTransferredFiles(t *testing.T) {
	// A file deleted outside the integration keeps its cache entry. When the
	// same content is copied again the destination checksum is unchanged, so
	// the entry would still match and the rehash job that reads the transfer
	// record would never be queued.
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		CheckPermission: func(context.Context, string, string, string) error { return nil },
		DeleteFiles:     func(context.Context, string, string, string, []int64) error { return nil },
	}
	ctx := context.Background()
	copied := globusNode("data/file.bin", "2026-09-22 10:00:00+00:00")
	copied.Action = tree.Copy
	copied.Attributes.DestinationFile = tree.DestinationFile{}
	updated := globusNode("data/other.bin", "2026-09-22 11:00:00+00:00")
	updated.Action = tree.Update
	deleted := globusNode("gone.bin", "2026-01-01 00:00:00+00:00")
	deleted.Action = tree.Delete
	untouched := globusNode("kept.bin", "2026-01-01 00:00:00+00:00")
	stale := func(ts string) calculatedHashes {
		return calculatedHashes{LocalHashType: types.Md5, LocalHashValue: "md5-from-dataverse", RemoteHashes: map[string]string{types.LastModified: ts}}
	}
	known := map[string]calculatedHashes{
		copied.Id:    stale("2020-01-01 00:00:00+00:00"),
		updated.Id:   stale("2020-01-01 00:00:00+00:00"),
		deleted.Id:   stale(deleted.Attributes.RemoteHash),
		untouched.Id: stale(untouched.Attributes.RemoteHash),
	}
	job := Job{PersistentId: testPid, Plugin: "globus", WritableNodes: map[string]tree.Node{
		copied.Id: copied, updated.Id: updated, deleted.Id: deleted,
	}}

	out, err := doPersistNodeMap(ctx, nil, job, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.WritableNodes) != 0 {
		t.Errorf("expected the globus job to hand every node to the transfer, got %v", out.WritableNodes)
	}
	for _, id := range []string{copied.Id, updated.Id, deleted.Id} {
		if _, ok := known[id]; ok {
			t.Errorf("expected the cached hash of %v to be dropped", id)
		}
	}
	if _, ok := known[untouched.Id]; !ok {
		t.Error("expected the cached hash of a file outside the job to be kept")
	}
	if v := config.GetRedis().Get(ctx, types.GlobusTransferKey(testPid, copied.Id)).Val(); v != "" {
		t.Errorf("expected no transfer record before the transfer is accepted, got %q", v)
	}
	// Once the identical content is back in Dataverse, the job must run.
	arrived := copied
	arrived.Attributes.DestinationFile = globusNode(copied.Id, "").Attributes.DestinationFile
	if value, needsJob := resolveDestinationHash(arrived, known[arrived.Id], ""); value != "?" || !needsJob {
		t.Errorf("expected the re-uploaded file to need a rehash job, got %q needsJob=%v", value, needsJob)
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
