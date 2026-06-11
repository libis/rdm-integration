package globus

import (
	"context"
	"encoding/json"
	"integration/app/plugin/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveDefaultDirectory(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "server_default_template_root",
			in:   "/{server_default}/",
			want: "/~/",
		},
		{
			name: "encoded_server_default_template_root",
			in:   "/%7Bserver_default%7D/",
			want: "/~/",
		},
		{
			name: "regular_path",
			in:   "/home/data/",
			want: "/home/data/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDefaultDirectory(tt.in)
			if got != tt.want {
				t.Fatalf("resolveDefaultDirectory(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOptionsReturnsEmptyWhenEndpointNotFound(t *testing.T) {
	t.Parallel()

	lsCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{
				Code:    "EndpointNotFound",
				Message: "No such endpoint with legacy name 'start'",
			})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			lsCalled = true
			_ = json.NewEncoder(w).Encode(Response{
				Code:    "EndpointNotFound",
				Message: "No such endpoint with legacy name 'start'",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "start",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("Options returned %d items, want 0", len(items))
	}
	if lsCalled {
		t.Fatal("expected no ls call when endpoint lookup reports EndpointNotFound")
	}
}

func TestOptionsReturnsErrorWhenPermissionDenied(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{
			Code:    "PermissionDenied",
			Message: "No effective ACL rules on endpoint",
		})
	}))
	defer server.Close()

	_, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "295a3b22-bfaa-43ed-b955-b6ccac71e0f1",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err == nil {
		t.Fatal("expected PermissionDenied error, got nil")
	}
	if !strings.Contains(err.Error(), "PermissionDenied") {
		t.Fatalf("expected PermissionDenied in error, got: %v", err)
	}
}

// TestOptionsBuildsHierarchyForResolvedAbsolutePath simulates a well-behaved
// endpoint (e.g. a GCS personal endpoint or a properly configured mapped
// collection) that resolves "/~/" to the user's actual home and returns it as
// the response's absolute_path. The picker should pre-expand the hierarchy
// down to that home directory and pre-select it.
func TestOptionsBuildsHierarchyForResolvedAbsolutePath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{
				DefaultDirectory: "/~/",
			})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			switch path {
			case "/~/":
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/home/alice/",
					Data: []Data{
						{Name: "data", Type: "dir"},
						{Name: "notes.txt", Type: "file"},
					},
				})
			default:
				t.Errorf("unexpected ls path: %q", path)
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "personal-linux",
		Url:      server.URL,
		Token:    "token",
		User:     "alice",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected single root node, got %d: %+v", len(items), items)
	}
	root := items[0]
	if root.Label != "home" {
		t.Fatalf("root label = %q, want %q", root.Label, "home")
	}
	if !root.Expanded || root.Selected {
		t.Fatalf("root expanded=%v selected=%v, want expanded=true selected=false", root.Expanded, root.Selected)
	}
	if len(root.Children) != 1 {
		t.Fatalf("home should have exactly 1 child, got %d", len(root.Children))
	}
	alice := root.Children[0]
	if alice.Label != "alice" || !alice.Expanded || !alice.Selected {
		t.Fatalf("alice node = %+v, want label=alice expanded=true selected=true", alice)
	}
	if len(alice.Children) != 1 || alice.Children[0].Label != "data" {
		t.Fatalf("alice should have 1 child 'data' (folders only), got %+v", alice.Children)
	}
}

// TestOptionsHandlesIrodsEchoedAbsolutePath reproduces the KU Leuven iRODS
// regression: the Globus iRODS connector lists "/~/" successfully but
// returns absolute_path = "/~/" unchanged. The previous implementation
// then created a single meaningless "~" node. The fix should detect the
// echoed shorthand and return the listed children flat, with NO node marked
// as selected (we don't want to nudge the user into writing under "/").
func TestOptionsHandlesIrodsEchoedAbsolutePath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{
				DefaultDirectory: "/~/",
			})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			switch path {
			case "/~/":
				// iRODS Globus connector quirk: absolute_path echoes the input.
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/~/",
					Data: []Data{
						{Name: "Lirias1-PROD.rdp", Type: "file"},
						{Name: "README.md", Type: "file"},
						{Name: "datasets", Type: "dir"},
					},
				})
			case "/":
				// Root listing also returns its echo.
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/",
					Data: []Data{
						{Name: "ghum", Type: "dir"},
						{Name: "image", Type: "dir"},
					},
				})
			default:
				t.Errorf("unexpected ls path: %q", path)
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "ghum.irods.icts.kuleuven.be",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected non-empty items, got 0")
	}
	for _, it := range items {
		if it.Selected {
			t.Fatalf("no node should be auto-selected when home cannot be resolved, got selected: %+v", it)
		}
		if it.Label == "~" {
			t.Fatalf("must not present a single meaningless '~' node, items: %+v", items)
		}
	}
	// We should see the /~/ listing's directory entries flat — at minimum the
	// "datasets" folder. Files are filtered out by the folder picker.
	foundDatasets := false
	for _, it := range items {
		if it.Label == "datasets" {
			foundDatasets = true
		}
	}
	if !foundDatasets {
		t.Fatalf("expected 'datasets' folder in flat listing, got: %+v", items)
	}
}

// TestOptionsResolvesIrodsHomeViaResponseAbsolutePath covers the happy-path
// variation of the iRODS case: when Globus *does* resolve "/~/" to a real
// path on the response, build the full hierarchy down to it.
func TestOptionsResolvesIrodsHomeViaResponseAbsolutePath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{
				DefaultDirectory: "/{server_default}/",
			})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			if path != "/~/" {
				t.Errorf("unexpected ls path: %q", path)
				http.Error(w, "unexpected", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(Response{
				AbsolutePath: "/ghum/home/u0050020/",
				Data: []Data{
					{Name: "datasets", Type: "dir"},
					{Name: "results", Type: "dir"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "ghum.irods.icts.kuleuven.be",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected single root node, got %d: %+v", len(items), items)
	}
	if items[0].Label != "ghum" {
		t.Fatalf("root label = %q, want %q", items[0].Label, "ghum")
	}
	// Walk down: ghum -> home -> u0050020 (selected)
	cur := items[0]
	wantPath := []string{"ghum", "home", "u0050020"}
	for i, expectedLabel := range wantPath {
		if cur.Label != expectedLabel {
			t.Fatalf("level %d label = %q, want %q", i, cur.Label, expectedLabel)
		}
		if !cur.Expanded {
			t.Fatalf("level %d (%q) should be expanded", i, expectedLabel)
		}
		isLeaf := i == len(wantPath)-1
		if isLeaf {
			if !cur.Selected {
				t.Fatalf("leaf %q should be selected", expectedLabel)
			}
			if len(cur.Children) != 2 {
				t.Fatalf("leaf %q should have 2 children, got %d", expectedLabel, len(cur.Children))
			}
		} else {
			if cur.Selected {
				t.Fatalf("ancestor %q must not be selected", expectedLabel)
			}
			if len(cur.Children) != 1 {
				t.Fatalf("ancestor %q should have 1 child, got %d", expectedLabel, len(cur.Children))
			}
			cur = cur.Children[0]
		}
	}
}

// TestOptionsFallsBackToRootWhenHomeNotFound covers the case where /~/ is
// rejected by the endpoint (NotFound). The picker should fall through to
// listing root and present it flat.
func TestOptionsFallsBackToRootWhenHomeNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			switch path {
			case "/~/":
				_ = json.NewEncoder(w).Encode(Response{
					Code:    "ClientError.NotFound",
					Message: "no such directory",
				})
			case "/":
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/",
					Data: []Data{
						{Name: "shared", Type: "dir"},
						{Name: "public", Type: "dir"},
					},
				})
			default:
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "public-endpoint",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 root-level items, got %d: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Selected {
			t.Fatalf("no node should be auto-selected for root fallback, got: %+v", it)
		}
	}
}

// TestOptionsFallsThroughWhenHomeListingHasNoFolders covers the iRODS-quirk
// edge case where /~/ lists successfully but contains only files (no
// subfolders), and root listing has folders. Without falling through, the
// picker would render an empty list — useless to the user. The picker should
// prefer the candidate that yields navigable folders.
func TestOptionsFallsThroughWhenHomeListingHasNoFolders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			switch path {
			case "/~/":
				// Echoed absolute_path AND only files in home — common iRODS layout.
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/~/",
					Data: []Data{
						{Name: "Lirias1-PROD.rdp", Type: "file"},
						{Name: "README.md", Type: "file"},
					},
				})
			case "/":
				_ = json.NewEncoder(w).Encode(Response{
					AbsolutePath: "/",
					Data: []Data{
						{Name: "ghum", Type: "dir"},
						{Name: "image", Type: "dir"},
					},
				})
			default:
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "ghum.irods.icts.kuleuven.be",
		Url:      server.URL,
		Token:    "token",
		User:     "u0050020",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected to fall through to root listing (2 items), got %d: %+v", len(items), items)
	}
	labels := []string{items[0].Label, items[1].Label}
	if !((labels[0] == "ghum" && labels[1] == "image") || (labels[0] == "image" && labels[1] == "ghum")) {
		t.Fatalf("expected ghum and image at root, got %v", labels)
	}
}

// TestOptionsBuildsHierarchyForExplicitDefaultDirectory covers a personal
// endpoint with an explicit absolute DefaultDirectory like "/home/me/data/".
func TestOptionsBuildsHierarchyForExplicitDefaultDirectory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/endpoint/"):
			_ = json.NewEncoder(w).Encode(Response{
				DefaultDirectory: "/home/me/data/",
			})
		case strings.HasPrefix(r.URL.Path, "/operation/endpoint/"):
			path := r.URL.Query().Get("path")
			if path != "/home/me/data/" {
				t.Errorf("unexpected ls path: %q", path)
				http.Error(w, "unexpected", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(Response{
				AbsolutePath: "/home/me/data/",
				Data: []Data{
					{Name: "subset", Type: "dir"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	items, err := Options(context.Background(), types.OptionsRequest{
		PluginId: "globus",
		Plugin:   "globus",
		RepoName: "personal-linux",
		Url:      server.URL,
		Token:    "token",
		User:     "me",
	})
	if err != nil {
		t.Fatalf("Options error: %v", err)
	}
	if len(items) != 1 || items[0].Label != "home" {
		t.Fatalf("expected root 'home', got %+v", items)
	}
	cur := items[0]
	for _, want := range []string{"home", "me", "data"} {
		if cur.Label != want {
			t.Fatalf("expected label %q at this level, got %q", want, cur.Label)
		}
		if want == "data" {
			if !cur.Selected || !cur.Expanded {
				t.Fatalf("data should be selected+expanded: %+v", cur)
			}
			break
		}
		if len(cur.Children) != 1 {
			t.Fatalf("expected exactly 1 child at %q, got %d", want, len(cur.Children))
		}
		cur = cur.Children[0]
	}
}

// TestBuildHierarchyRefusesPlaceholders directly tests that buildHierarchy
// will not produce a "~" or "{server_default}" wrapping node — defense in
// depth in case a caller forgets to gate on isMeaningfulHierarchyPath.
func TestBuildHierarchyRefusesPlaceholders(t *testing.T) {
	tests := []struct {
		name      string
		targetDir string
	}{
		{"echoed_home_shorthand", "/~/"},
		{"server_default_template", "/{server_default}/"},
		{"empty", ""},
		{"root", "/"},
	}
	children := []types.SelectItem{
		{Label: "datasets", Value: "/datasets/"},
		{Label: "results", Value: "/results/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHierarchy(tt.targetDir, children)
			if len(got) != len(children) {
				t.Fatalf("expected %d items returned flat, got %d: %+v", len(children), len(got), got)
			}
			for _, it := range got {
				if it.Label == "~" || strings.Contains(it.Label, "{") {
					t.Fatalf("buildHierarchy must not produce placeholder labels, got %+v", got)
				}
			}
		})
	}
}

// TestIsMeaningfulHierarchyPath covers the placeholder-detection helper.
func TestIsMeaningfulHierarchyPath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"/", false},
		{"/~/", false},
		{"/{server_default}/", false},
		{"/{server_default}/u0050020/", false},
		{"/home/user/", true},
		{"/ghum/home/u0050020/", true},
		{"/C:/Users/me/", true},
	}
	for _, tt := range tests {
		if got := isMeaningfulHierarchyPath(tt.in); got != tt.want {
			t.Errorf("isMeaningfulHierarchyPath(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
