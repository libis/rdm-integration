package core

import (
	"context"
	"integration/app/config"
	"integration/app/testutil"
	"integration/app/tree"
	"testing"
)

func TestNewJobClearsPreviousErrorButRetryPreservesIt(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()
	ctx := context.Background()
	job := Job{
		PersistentId:  "doi:10.1/TEST",
		WritableNodes: map[string]tree.Node{"file.bin": {Id: "file.bin"}},
	}
	errorKey := "error " + job.PersistentId
	fr.Set(ctx, errorKey, "previous failure", FileNamesInCacheDuration)
	if err := AddJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if got := fr.Get(ctx, errorKey).Val(); got != "" {
		t.Error("new job inherited the previous job's error")
	}
	fr.Set(ctx, errorKey, "current failure", FileNamesInCacheDuration)
	if err := AddJob(ctx, job); err == nil {
		t.Fatal("expected a competing job to be rejected")
	}
	if err := addJob(ctx, job, false); err != nil {
		t.Fatal(err)
	}
	if got := fr.Get(ctx, errorKey).Val(); got != "current failure" {
		t.Error("a rejected submission or retry erased the current job's error")
	}
}
