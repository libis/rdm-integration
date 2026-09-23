// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"context"
	"errors"
	"integration/app/config"
	"integration/app/plugin/types"
	"integration/app/testutil"
	"integration/app/tree"
	"testing"
	"testing/synctest"
	"time"
)

func TestDoFlushReturnsErrorAndRequeuesFailedFiles(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		SaveAfterDirectUpload: func(ctx context.Context, replace bool, token, user, persistentId string, storageIdentifiers []string, nodes []tree.Node) (map[string]bool, error) {
			return nil, types.NewUnrecoverableError(errors.New("1 out of 1 files were not registered by the server"))
		},
	}

	job := Job{PersistentId: "doi:10.1/TEST", WritableNodes: map[string]tree.Node{}}
	toAddNodes := &[]tree.Node{{Id: "file.bin"}}
	toAddIdentifiers := &[]string{"s3://bucket:xyz"}
	knownHashes := map[string]calculatedHashes{"file.bin": {LocalHashValue: "abc"}}
	marker := job.PersistentId + " -> file.bin"
	fr.Set(context.Background(), marker, types.Written, FileNamesInCacheDuration)
	fr.Set(context.Background(), "file.bin", "unrelated key", 0)

	err := doFlush(context.Background(), toAddNodes, &[]tree.Node{}, &job, knownHashes, toAddIdentifiers, &[]string{})

	if err == nil {
		t.Fatal("expected the flush error to be returned so the job loop can see it")
	}
	if !types.IsUnrecoverable(err) {
		t.Error("expected the unrecoverable classification to survive doFlush")
	}
	if _, ok := job.WritableNodes["file.bin"]; !ok {
		t.Error("expected the failed file to be re-queued in WritableNodes")
	}
	if _, ok := knownHashes["file.bin"]; ok {
		t.Error("expected the failed file's known hashes to be dropped")
	}
	if got := fr.Get(context.Background(), marker).Val(); got != "" {
		t.Errorf("failed registration left a success marker: %q", got)
	}
	if got := fr.Get(context.Background(), "file.bin").Val(); got != "unrelated key" {
		t.Error("rollback deleted an unrelated key matching the bare filename")
	}
}

func TestPersistCleanupPreservesNewJobError(t *testing.T) {
	for _, plugin := range []string{"local", "globus"} {
		t.Run(plugin, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fr := testutil.NewFakeRedis()
				config.SetRedis(fr)
				defer fr.Reset()
				savedDestination := Destination
				defer func() { Destination = savedDestination }()
				Destination = DestinationPlugin{
					CheckPermission: func(context.Context, string, string, string) error { return nil },
					DeleteFiles:     func(context.Context, string, string, string, []int64) error { return nil },
				}
				job := Job{
					PersistentId: "doi:10.1/TEST", Plugin: plugin,
					WritableNodes: map[string]tree.Node{"file.bin": {Id: "file.bin", Action: tree.Delete}},
				}
				if _, err := doPersistNodeMap(context.Background(), nil, job, map[string]calculatedHashes{}); err != nil {
					t.Fatal(err)
				}
				// A deferred registration or Globus cleanup can fail after marker
				// cleanup is scheduled. Its notification must remain available.
				errorKey := "error " + job.PersistentId
				fr.Set(context.Background(), errorKey, "registration failed", FileNamesInCacheDuration)
				time.Sleep(11 * time.Second)
				if got := fr.Get(context.Background(), errorKey).Val(); got != "registration failed" {
					t.Error("marker cleanup erased the unread job error")
				}
				if got := fr.Get(context.Background(), job.PersistentId+" -> file.bin").Val(); got != "" {
					t.Error("file marker was not cleaned up")
				}
			})
		})
	}
}

func TestDoFlushSuccessReturnsNil(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	savedDestination := Destination
	defer func() { Destination = savedDestination }()
	Destination = DestinationPlugin{
		SaveAfterDirectUpload: func(ctx context.Context, replace bool, token, user, persistentId string, storageIdentifiers []string, nodes []tree.Node) (map[string]bool, error) {
			return map[string]bool{"s3://bucket:xyz": true}, nil
		},
	}

	job := Job{PersistentId: "doi:10.1/TEST", WritableNodes: map[string]tree.Node{}}
	toAddNodes := &[]tree.Node{{Id: "file.bin"}}
	toAddIdentifiers := &[]string{"s3://bucket:xyz"}

	if err := doFlush(context.Background(), toAddNodes, &[]tree.Node{}, &job, map[string]calculatedHashes{}, toAddIdentifiers, &[]string{}); err != nil {
		t.Fatalf("expected no error on a successful flush, got: %v", err)
	}
	if len(job.WritableNodes) != 0 {
		t.Error("expected no re-queued files after a successful flush")
	}
}
