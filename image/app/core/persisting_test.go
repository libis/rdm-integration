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
