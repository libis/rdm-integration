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
