// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package core

import (
	"integration/app/plugin/types"
	"integration/app/tree"
	"testing"
)

func node(destId int64, destHash, destHashType, remoteHash, remoteHashType string) tree.Node {
	return tree.Node{
		Id: "folder/file.bin",
		Attributes: tree.Attributes{
			RemoteHash:     remoteHash,
			RemoteHashType: remoteHashType,
			DestinationFile: tree.DestinationFile{
				Id:       destId,
				Hash:     destHash,
				HashType: destHashType,
			},
		},
	}
}

func TestResolveDestinationHashDeletedOutsideIntegration(t *testing.T) {
	// file removed via the Dataverse UI: not in the listing, but a rehash is
	// still cached from when it existed — it must not appear present/equal
	cached := calculatedHashes{RemoteHashes: map[string]string{types.SHA256: "abc123"}}
	value, needsJob := resolveDestinationHash(node(0, "", "", "abc123", types.SHA256), cached, "")
	if value != "" || needsJob {
		t.Errorf("expected empty destination hash and no job for a file absent from the destination, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashCachedForExistingFile(t *testing.T) {
	cached := calculatedHashes{
		LocalHashType:  types.Md5,
		LocalHashValue: "d41d8cd9",
		RemoteHashes:   map[string]string{types.SHA256: "abc123"},
	}
	value, needsJob := resolveDestinationHash(node(42, "d41d8cd9", types.Md5, "abc123", types.SHA256), cached, "")
	if value != "abc123" || needsJob {
		t.Errorf("expected cached rehash for an existing unchanged file, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashReplacedOutsideIntegration(t *testing.T) {
	// file replaced via the Dataverse UI: the listing hash differs from the
	// one recorded in the cache, so the cached rehash describes old content
	// and a fresh rehash job is needed
	cached := calculatedHashes{
		LocalHashType:  types.Md5,
		LocalHashValue: "oldcontent",
		RemoteHashes:   map[string]string{types.SHA256: "abc123"},
	}
	value, needsJob := resolveDestinationHash(node(42, "newcontent", types.Md5, "abc123", types.SHA256), cached, "")
	if value != "?" || !needsJob {
		t.Errorf("expected a rehash job for externally replaced content, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashSameHashType(t *testing.T) {
	value, needsJob := resolveDestinationHash(node(42, "abc123", types.SHA256, "abc123", types.SHA256), calculatedHashes{}, "")
	if value != "abc123" || needsJob {
		t.Errorf("expected direct destination hash when hash types match, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashJustWrittenMarker(t *testing.T) {
	// a file uploaded moments ago may not be in the listing yet: the written
	// marker must still show it as present with the remote hash
	value, needsJob := resolveDestinationHash(node(0, "", "", "abc123", types.SHA256), calculatedHashes{}, types.Written)
	if value != "abc123" || needsJob {
		t.Errorf("expected the written marker to present the file as equal, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashJustDeletedMarker(t *testing.T) {
	cached := calculatedHashes{RemoteHashes: map[string]string{types.SHA256: "abc123"}}
	value, needsJob := resolveDestinationHash(node(42, "d41d8cd9", types.Md5, "abc123", types.SHA256), cached, types.Deleted)
	if value != "" || needsJob {
		t.Errorf("expected the deleted marker to clear the destination hash, got %q needsJob=%v", value, needsJob)
	}
}

func TestResolveDestinationHashNeedsRehashJob(t *testing.T) {
	// existing file, no cached hash in the remote hash type: a rehash job is needed
	value, needsJob := resolveDestinationHash(node(42, "d41d8cd9", types.Md5, "abc123", types.SHA256), calculatedHashes{}, "")
	if value != "?" || !needsJob {
		t.Errorf("expected a rehash job for an existing file without cached hash, got %q needsJob=%v", value, needsJob)
	}
}
