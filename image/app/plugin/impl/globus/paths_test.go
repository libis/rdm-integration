package globus

import (
	"context"
	"encoding/json"
	"integration/app/plugin/types"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestBuildCandidatePaths(t *testing.T) {
	tests := []struct {
		name       string
		defaultDir string
		want       []string
	}{
		{"empty", "", []string{"/~/", "/"}},
		{"home_shorthand", "/~/", []string{"/~/", "/"}},
		{"server_default_template", "/{server_default}/", []string{"/~/", "/"}},
		{"encoded_server_default_template", "/%7Bserver_default%7D/", []string{"/~/", "/"}},
		{"explicit_path", "/home/me/data/", []string{"/home/me/data/", "/~/", "/"}},
		{"root", "/", []string{"/", "/~/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCandidatePaths(Response{DefaultDirectory: tt.defaultDir})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildCandidatePaths(%q) = %v, want %v", tt.defaultDir, got, tt.want)
			}
		})
	}
}

func TestPickResolvedDir(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		absolute  string
		items     []types.SelectItem
		want      string
	}{
		{"gcp_linux_home", "/~/", "/home/alice/", []types.SelectItem{{Label: "data", Value: "/home/alice/data/"}}, "/home/alice/"},
		{"gcp_macos_home", "/~/", "/Users/alice/", []types.SelectItem{{Label: "Documents", Value: "/Users/alice/Documents/"}}, "/Users/alice/"},
		{"gcp_windows_home", "/~/", "/C/Users/alice/", []types.SelectItem{{Label: "Documents", Value: "/C/Users/alice/Documents/"}}, "/C/Users/alice/"},
		{"irods_echoed_home", "/~/", "/~/", []types.SelectItem{{Label: "datasets", Value: "/~/datasets/"}}, "/~/"},
		{"irods_resolved_home", "/~/", "/ghum/home/u0050020/", []types.SelectItem{{Label: "datasets", Value: "/ghum/home/u0050020/datasets/"}}, "/ghum/home/u0050020/"},
		{"guest_root", "/", "/", []types.SelectItem{{Label: "shared", Value: "/shared/"}}, "/"},
		{"root_no_absolute_path", "/", "", []types.SelectItem{{Label: "shared", Value: "/shared/"}}, "/"},
		{"no_absolute_path_uses_child_parent", "/~/", "", []types.SelectItem{{Label: "data", Value: "/home/alice/data/"}}, "/home/alice/"},
		{"no_absolute_path_no_items", "/~/", "", nil, "/~/"},
		{"absolute_path_without_trailing_slash", "/~/", "/home/alice", nil, "/home/alice/"},
		{"explicit_default_dir_echoed", "/home/me/data/", "/home/me/data/", nil, "/home/me/data/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickResolvedDir(tt.candidate, tt.absolute, tt.items)
			if got != tt.want {
				t.Fatalf("pickResolvedDir(%q, %q) = %q, want %q", tt.candidate, tt.absolute, got, tt.want)
			}
		})
	}
}

func TestBuildHierarchy(t *testing.T) {
	children := []types.SelectItem{{Label: "data", Value: "/x/data/"}}
	tests := []struct {
		name      string
		targetDir string
		labels    []string
		values    []string
	}{
		{"gcp_linux", "/home/alice/", []string{"home", "alice"}, []string{"/home/", "/home/alice/"}},
		{"gcp_macos", "/Users/alice/", []string{"Users", "alice"}, []string{"/Users/", "/Users/alice/"}},
		{"gcp_windows", "/C/Users/alice/", []string{"C", "Users", "alice"}, []string{"/C/", "/C/Users/", "/C/Users/alice/"}},
		{"irods", "/ghum/home/u0050020/", []string{"ghum", "home", "u0050020"}, []string{"/ghum/", "/ghum/home/", "/ghum/home/u0050020/"}},
		{"missing_slashes", "data", []string{"data"}, []string{"/data/"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHierarchy(tt.targetDir, children)
			if len(got) != 1 {
				t.Fatalf("expected single top node, got %+v", got)
			}
			cur := got[0]
			for i, label := range tt.labels {
				last := i == len(tt.labels)-1
				if cur.Label != label || cur.Value != tt.values[i] || !cur.Expanded || cur.Selected != last {
					t.Fatalf("level %d = %+v, want label=%q value=%q expanded=true selected=%v", i, cur, label, tt.values[i], last)
				}
				if last {
					if !reflect.DeepEqual(cur.Children, children) {
						t.Fatalf("target children = %+v, want %+v", cur.Children, children)
					}
					break
				}
				if len(cur.Children) != 1 {
					t.Fatalf("level %d should have 1 child, got %+v", i, cur.Children)
				}
				cur = cur.Children[0]
			}
		})
	}
}

// lsServer serves the endpoint document and the given ls responses keyed by
// requested path. Paths not in ls return ClientError.NotFound.
func lsServer(t *testing.T, defaultDir string, ls map[string]Response) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{DefaultDirectory: defaultDir})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			res, ok := ls[r.URL.Query().Get("path")]
			if !ok {
				res = Response{Code: "ClientError.NotFound", Message: "no such directory"}
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			http.NotFound(w, r)
		}
	}))
}

// flatten returns "label=value[*selected][+expanded]" for every node, depth first.
func flatten(items []types.SelectItem) []string {
	out := []string{}
	for _, it := range items {
		s := it.Label + "=" + it.Value.(string)
		if it.Selected {
			s += "*"
		}
		if it.Expanded {
			s += "+"
		}
		out = append(out, s)
		out = append(out, flatten(it.Children)...)
	}
	return out
}

var (
	rootShared = Response{AbsolutePath: "/", Data: []Data{{Name: "shared", Type: "dir"}, {Name: "readme.txt", Type: "file"}}}
	rootIrods  = Response{AbsolutePath: "/", Data: []Data{{Name: "ghum", Type: "dir"}, {Name: "image", Type: "dir"}}}
)

func TestOptionsPerEndpointType(t *testing.T) {
	tests := []struct {
		name       string
		defaultDir string
		ls         map[string]Response
		want       []string
	}{
		{
			name:       "gcp_linux",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/home/alice/", Data: []Data{{Name: "data", Type: "dir"}, {Name: "notes.txt", Type: "file"}}},
				"/":   rootShared,
			},
			want: []string{"/=/+", "home=/home/+", "alice=/home/alice/*+", "data=/home/alice/data/"},
		},
		{
			name:       "gcp_macos",
			defaultDir: "",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/Users/alice/", Data: []Data{{Name: "Documents", Type: "dir"}}},
				"/":   {AbsolutePath: "/", Data: []Data{{Name: "Users", Type: "dir"}}},
			},
			want: []string{"/=/+", "Users=/Users/+", "alice=/Users/alice/*+", "Documents=/Users/alice/Documents/"},
		},
		{
			name:       "gcp_windows",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/C/Users/alice/", Data: []Data{{Name: "Documents", Type: "dir"}}},
				"/":   {AbsolutePath: "/", Data: []Data{{Name: "C", Type: "dir"}, {Name: "D", Type: "dir"}}},
			},
			want: []string{"/=/+", "C=/C/+", "Users=/C/Users/+", "alice=/C/Users/alice/*+", "Documents=/C/Users/alice/Documents/"},
		},
		{
			name:       "guest_collection",
			defaultDir: "/",
			ls:         map[string]Response{"/": rootShared},
			want:       []string{"/=/+", "shared=/shared/"},
		},
		{
			name:       "mapped_posix",
			defaultDir: "/{server_default}/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/home/alice/", Data: []Data{{Name: "data", Type: "dir"}}},
				"/":   rootShared,
			},
			want: []string{"/=/+", "home=/home/+", "alice=/home/alice/*+", "data=/home/alice/data/"},
		},
		{
			name:       "mapped_irods_echoed_home",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/~/", Data: []Data{{Name: "README.md", Type: "file"}, {Name: "datasets", Type: "dir"}}},
				"/":   rootIrods,
			},
			want: []string{"/=/", "~=/~/+", "datasets=/~/datasets/"},
		},
		{
			name:       "mapped_irods_echoed_home_without_folders",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/~/", Data: []Data{{Name: "README.md", Type: "file"}}},
				"/":   rootIrods,
			},
			want: []string{"/=/+", "ghum=/ghum/", "image=/image/"},
		},
		{
			name:       "mapped_irods_resolved_home",
			defaultDir: "/{server_default}/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/ghum/home/u0050020/", Data: []Data{{Name: "datasets", Type: "dir"}}},
				"/":   rootIrods,
			},
			want: []string{"/=/+", "ghum=/ghum/+", "home=/ghum/home/+", "u0050020=/ghum/home/u0050020/*+", "datasets=/ghum/home/u0050020/datasets/"},
		},
		{
			name:       "home_not_found",
			defaultDir: "",
			ls:         map[string]Response{"/": rootShared},
			want:       []string{"/=/+", "shared=/shared/"},
		},
		{
			name:       "home_empty_and_root_empty",
			defaultDir: "",
			ls:         map[string]Response{"/~/": {AbsolutePath: "/~/"}, "/": {AbsolutePath: "/"}},
			want:       []string{"/=/", "~=/~/+"},
		},
		{
			name:       "home_resolves_to_virtual_root",
			defaultDir: "/{server_default}/",
			ls:         map[string]Response{"/~/": rootShared, "/": rootShared},
			want:       []string{"/=/+", "shared=/shared/"},
		},
		{
			name:       "empty_guest_collection",
			defaultDir: "/",
			ls:         map[string]Response{"/": {AbsolutePath: "/"}},
			want:       []string{"/=/+"},
		},
		{
			name:       "empty_resolved_home_with_inaccessible_root",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {AbsolutePath: "/home/alice/"},
				"/":   {Code: "PermissionDenied", Message: "root is not accessible"},
			},
			want: []string{"/=/+", "home=/home/+", "alice=/home/alice/*+"},
		},
		{
			name:       "home_denied_with_accessible_root",
			defaultDir: "/~/",
			ls: map[string]Response{
				"/~/": {Code: "PermissionDenied", Message: "home is not accessible"},
				"/":   rootShared,
			},
			want: []string{"/=/+", "shared=/shared/"},
		},
		{
			name:       "windows_mapped_network_drive",
			defaultDir: "/Z/Projects/",
			ls: map[string]Response{
				"/Z/Projects/": {AbsolutePath: "/Z/Projects/", Data: []Data{{Name: "data", Type: "dir"}}},
			},
			want: []string{"/=/+", "Z=/Z/+", "Projects=/Z/Projects/*+", "data=/Z/Projects/data/"},
		},
		{
			name:       "object_storage_bucket_default",
			defaultDir: "/bucket/project/",
			ls: map[string]Response{
				"/bucket/project/": {AbsolutePath: "/bucket/project/", Data: []Data{{Name: "data", Type: "dir"}}},
			},
			want: []string{"/=/+", "bucket=/bucket/+", "project=/bucket/project/*+", "data=/bucket/project/data/"},
		},
		{
			name:       "explicit_default_without_absolute_path",
			defaultDir: "/shared/project/",
			ls: map[string]Response{
				"/shared/project/": {Data: []Data{{Name: "data", Type: "dir"}}},
			},
			want: []string{"/=/+", "shared=/shared/+", "project=/shared/project/*+", "data=/shared/project/data/"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := lsServer(t, tt.defaultDir, tt.ls)
			defer server.Close()
			items, err := Options(context.Background(), types.OptionsRequest{
				PluginId: "globus", Plugin: "globus", RepoName: "ep", Url: server.URL, Token: "token", User: "alice",
			})
			if err != nil {
				t.Fatalf("Options error: %v", err)
			}
			if got := flatten(items); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tree = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOptionsExpandsRootAndAnotherWindowsDrive(t *testing.T) {
	server := lsServer(t, "/~/", map[string]Response{
		"/~/": {AbsolutePath: "/C/Users/alice/"},
		"/": {
			AbsolutePath: "/",
			Data:         []Data{{Name: "C", Type: "dir"}, {Name: "D", Type: "dir"}},
		},
		"/D/": {
			AbsolutePath: "/D/",
			Data:         []Data{{Name: "Shared", Type: "dir"}, {Name: "notes.txt", Type: "file"}},
		},
	})
	defer server.Close()
	params := types.OptionsRequest{RepoName: "ep", Url: server.URL, Token: "token"}
	for _, tt := range []struct {
		option string
		want   []string
	}{
		{"", []string{"/=/+", "C=/C/+", "Users=/C/Users/+", "alice=/C/Users/alice/*+"}},
		{"/", []string{"C=/C/", "D=/D/"}},
		{"/D/", []string{"Shared=/D/Shared/"}},
	} {
		params.Option = tt.option
		items, err := Options(context.Background(), params)
		if err != nil {
			t.Fatalf("Options(%q) error: %v", tt.option, err)
		}
		if got := flatten(items); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("Options(%q) = %v, want %v", tt.option, got, tt.want)
		}
	}
}
