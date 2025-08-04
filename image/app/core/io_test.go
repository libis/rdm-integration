// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"integration/app/plugin/types"
	"reflect"
	"strings"
	"testing"
)

func TestGetHashOptimization(t *testing.T) {
	tests := []struct {
		hashType     string
		fileSize     int64
		expectedType string
		shouldError  bool
	}{
		{"MD5", 1024, "md5", false},
		{"md5", 1024, "md5", false},
		{"SHA-1", 1024, "sha1", false},
		{"sha1", 1024, "sha1", false},
		{"SHA256", 1024, "sha256", false},
		{"sha256", 1024, "sha256", false},
		{"SHA512", 1024, "sha512", false},
		{"sha512", 1024, "sha512", false},
		{"git-hash", 1024, "sha1", false}, // git-hash uses sha1 internally
		{"quickXorHash", 1024, "QuickXorHash", false},
		{"FileSize", 1024, "FileSizeHash", false},
		{"invalid", 1024, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.hashType, func(t *testing.T) {
			hasher, err := getHash(tt.hashType, tt.fileSize)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for hash type %s, but got none", tt.hashType)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for hash type %s: %v", tt.hashType, err)
				return
			}

			if hasher == nil {
				t.Errorf("Expected hasher for hash type %s, but got nil", tt.hashType)
				return
			}

			// Verify the correct hasher type was returned by checking interface
			hasherTypeName := reflect.TypeOf(hasher).String()
			switch tt.expectedType {
			case "md5":
				if !strings.Contains(hasherTypeName, "md5") {
					t.Errorf("Expected MD5 hasher, got %s", hasherTypeName)
				}
			case "sha1":
				if !strings.Contains(hasherTypeName, "sha1") {
					t.Errorf("Expected SHA1 hasher, got %s", hasherTypeName)
				}
			case "sha256":
				if !strings.Contains(hasherTypeName, "sha256") {
					t.Errorf("Expected SHA256 hasher, got %s", hasherTypeName)
				}
			case "sha512":
				if !strings.Contains(hasherTypeName, "sha512") {
					t.Errorf("Expected SHA512 hasher, got %s", hasherTypeName)
				}
			case "QuickXorHash":
				if _, ok := hasher.(*QuickXorHash); !ok {
					t.Errorf("Expected QuickXorHash, got %s", hasherTypeName)
				}
			case "FileSizeHash":
				if _, ok := hasher.(*FileSizeHash); !ok {
					t.Errorf("Expected FileSizeHash, got %s", hasherTypeName)
				}
			}
		})
	}
}

func TestGetHashConstants(t *testing.T) {
	// Test that constants from types package work correctly
	testCases := []struct {
		constant string
		expected bool
	}{
		{types.Md5, true},
		{types.SHA1, true},
		{types.SHA256, true},
		{types.SHA512, true},
		{types.GitHash, true},
		{types.QuickXorHash, true},
		{types.FileSize, true},
	}

	for _, tc := range testCases {
		t.Run(tc.constant, func(t *testing.T) {
			hasher, err := getHash(tc.constant, 1024)
			if tc.expected && err != nil {
				t.Errorf("Expected no error for constant %s, got: %v", tc.constant, err)
			}
			if tc.expected && hasher == nil {
				t.Errorf("Expected hasher for constant %s, got nil", tc.constant)
			}
		})
	}
}
