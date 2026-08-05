// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package irods

import (
	"errors"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"
	"testing"

	"github.com/cyverse/go-irodsclient/fs"
	"github.com/cyverse/go-irodsclient/irods/common"
	irodstypes "github.com/cyverse/go-irodsclient/irods/types"
)

func TestNormalizeHashType(t *testing.T) {
	testCases := []struct {
		algorithm string
		expected  string
		expectErr bool
	}{
		{"md5", types.Md5, false},
		{"MD5", types.Md5, false},
		{"SHA-256", types.SHA256, false},
		{"sha-256", types.SHA256, false},
		{"SHA-512", types.SHA512, false},
		{"ADLER-32", "", true},
		{"", "", true},
	}
	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			hashType, err := normalizeHashType(tc.algorithm)
			if tc.expectErr {
				if err == nil {
					t.Errorf("expected an error for %q, got %q", tc.algorithm, hashType)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error for %q, got: %v", tc.algorithm, err)
			}
			if hashType != tc.expected {
				t.Errorf("expected %q for %q, got %q", tc.expected, tc.algorithm, hashType)
			}
		})
	}
}

func TestHashUsesChecksumFromListing(t *testing.T) {
	entry := &fs.Entry{
		CheckSumAlgorithm: irodstypes.ChecksumAlgorithmMD5,
		CheckSum:          []byte{0x81, 0x5e, 0x03, 0x4c},
	}
	// new file (not in the destination node map): the listing checksum must be
	// used so that the transfer can be verified
	hashType, h, err := hash(nil, "/zone/folder", "sub/file.bin", map[string]tree.Node{}, entry)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if hashType != types.Md5 || h != "815e034c" {
		t.Errorf("expected MD5 815e034c from the listing, got %v %v", hashType, h)
	}
}

func TestHashNewFileWithoutListingChecksum(t *testing.T) {
	entry := &fs.Entry{}
	hashType, h, err := hash(nil, "/zone/folder", "sub/file.bin", map[string]tree.Node{}, entry)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if hashType != types.Md5 || h != types.NotNeeded {
		t.Errorf("expected the not-needed placeholder for a new file without checksum, got %v %v", hashType, h)
	}
}

func TestHashUnknownListingAlgorithmFallsBack(t *testing.T) {
	entry := &fs.Entry{
		CheckSumAlgorithm: irodstypes.ChecksumAlgorithm("ADLER-32"),
		CheckSum:          []byte{0x01, 0x02},
	}
	hashType, h, err := hash(nil, "/zone/folder", "sub/file.bin", map[string]tree.Node{}, entry)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if hashType != types.Md5 || h != types.NotNeeded {
		t.Errorf("expected fallback to the not-needed placeholder for an unsupported algorithm, got %v %v", hashType, h)
	}
}

func TestClassifyOpenError(t *testing.T) {
	if classifyOpenError("/zone/f.bin", nil) != nil {
		t.Error("expected nil to stay nil")
	}

	locked := irodstypes.NewIRODSError(common.HIERARCHY_ERROR)
	classified := classifyOpenError("/zone/f.bin", locked)
	if !types.IsUnrecoverable(classified) {
		t.Error("expected HIERARCHY_ERROR to be classified as unrecoverable")
	}
	if !strings.Contains(classified.Error(), "locked") {
		t.Errorf("expected an actionable message about the file being locked, got: %v", classified)
	}

	notFound := irodstypes.NewFileNotFoundError("/zone/f.bin")
	if !types.IsUnrecoverable(classifyOpenError("/zone/f.bin", notFound)) {
		t.Error("expected file-not-found to be classified as unrecoverable")
	}

	transient := errors.New("connection reset by peer")
	if types.IsUnrecoverable(classifyOpenError("/zone/f.bin", transient)) {
		t.Error("expected an unclassified error to stay retryable")
	}
}
