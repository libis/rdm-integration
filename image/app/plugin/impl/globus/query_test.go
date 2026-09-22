package globus

import (
	"context"
	"integration/app/plugin/types"
	"testing"
)

func TestToNodeMapTrimsSelectedFolder(t *testing.T) {
	tests := []struct {
		name     string
		folder   string
		entryDir string
		wantId   string
		wantPath string
	}{
		{"root_file_in_root", "/", "/", "f.txt", ""},
		{"root_file_in_subfolder", "/", "/sub/", "sub/f.txt", "sub"},
		{"root_file_in_nested_subfolder", "/", "/sub/deep/", "sub/deep/f.txt", "sub/deep"},
		{"home_file_in_root", "/home/alice/", "/home/alice/", "f.txt", ""},
		{"home_file_in_subfolder", "/home/alice/", "/home/alice/sub/", "sub/f.txt", "sub"},
		{"home_file_in_nested_subfolder", "/home/alice/", "/home/alice/sub/deep/", "sub/deep/f.txt", "sub/deep"},
		{"windows_file_in_subfolder", "/C/Users/alice/", "/C/Users/alice/sub/", "sub/f.txt", "sub"},
		{"irods_echoed_file_in_subfolder", "/~/datasets/", "/~/datasets/sub/", "sub/f.txt", "sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := []Entry{
				{Id: tt.entryDir + "f.txt/", Path: tt.entryDir, Name: "f.txt", IsDir: false},
				{Id: tt.entryDir + "dir/", Path: tt.entryDir, Name: "dir", IsDir: true},
			}
			nodes, err := toNodeMap(tt.folder, entries)
			if err != nil {
				t.Fatalf("toNodeMap error: %v", err)
			}
			if len(nodes) != 1 {
				t.Fatalf("expected 1 file node, got %+v", nodes)
			}
			node, ok := nodes[tt.wantId]
			if !ok {
				t.Fatalf("expected node id %q, got %+v", tt.wantId, nodes)
			}
			if node.Id != tt.wantId || node.Path != tt.wantPath || node.Name != "f.txt" {
				t.Fatalf("node = %+v, want id=%q path=%q name=f.txt", node, tt.wantId, tt.wantPath)
			}
		})
	}
}

// Exercise the HTTP listing, recursive relative paths and transfer manifest
// together; helper-only tests would miss a mismatch between these stages.
func TestQueryAndTransferEndpointPaths(t *testing.T) {
	tests := []struct {
		name   string
		folder string
	}{
		{"linux", "/home/alice/"},
		{"macos", "/Users/alice/"},
		{"windows", "/C/Users/alice/"},
		{"windows_network_drive", "/Z/Projects/"},
		{"mapped_posix", "/project/shared/"},
		{"irods", "/ghum/home/alice/"},
		{"irods_echoed_home", "/~/"},
		{"object_storage", "/bucket/prefix/"},
		{"guest_root", "/"},
		{"symlink_parent", "/home/alice/link/../data/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const rootName = "report %2F café.txt"
			server := lsServer(t, tt.folder, map[string]Response{
				tt.folder: {
					AbsolutePath: tt.folder,
					Data: []Data{
						{Name: rootName, Type: "file", LastModified: "2026-01-02 03:04:05+00:00", Size: 10},
						{Name: "sub", Type: "dir"},
					},
				},
				tt.folder + "sub/": {
					AbsolutePath: tt.folder + "sub/",
					Data:         []Data{{Name: "nested.txt", Type: "file", LastModified: "2026-02-03 04:05:06+00:00", Size: 20}},
				},
			})
			defer server.Close()
			nodes, err := Query(context.Background(), types.CompareRequest{
				Option: tt.folder, RepoName: "ep", Url: server.URL, Token: "token",
			}, nil)
			if err != nil {
				t.Fatalf("Query error: %v", err)
			}
			if len(nodes) != 2 || nodes[rootName].Name != rootName || nodes[rootName].Path != "" ||
				nodes["sub/nested.txt"].Name != "nested.txt" || nodes["sub/nested.txt"].Path != "sub" {
				t.Fatalf("unexpected relative file paths: %+v", nodes)
			}
			paths := []Path{{Id: "store:1", Path: "/dest/1"}, {Id: "store:2", Path: "/dest/2"}}
			data, files, transfers := transferItems(tt.folder, nodes, paths)
			if len(data) != 2 || len(files) != 2 || len(transfers) != 2 {
				t.Fatalf("unexpected transfer manifest: %+v %+v %+v", data, files, transfers)
			}
			for i, file := range files {
				id := file.FileName
				if file.DirectoryLabel != "" {
					id = file.DirectoryLabel + "/" + id
				}
				if data[i].SourcePath != tt.folder+id || data[i].DestinationPath != paths[i].Path ||
					file.StorageIdentifier != paths[i].Id || data[i].Recursive {
					t.Fatalf("transfer and metadata disagree: %+v %+v", data[i], file)
				}
				if file.Checksum.Type != types.LastModified || file.Checksum.Value != nodes[id].Attributes.RemoteHash {
					t.Fatalf("checksum metadata was lost: %+v", file)
				}
				if transfers[id].StorageIdentifier != paths[i].Id || transfers[id].LastModified != nodes[id].Attributes.RemoteHash {
					t.Fatalf("transfer record does not describe the file's object: %+v", transfers[id])
				}
			}
		})
	}
}
