package globus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
)

func TestNormalizeEndpointPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "decodes_server_default_prefix",
			in:   "/%7Bserver_default%7D/u0050020/",
			want: "/u0050020/",
		},
		{
			name: "handles_plain_server_default_prefix",
			in:   "/{server_default}/C:/Users/demo/",
			want: "/C:/Users/demo/",
		},
		{
			name: "handles_server_default_only",
			in:   "/{server_default}",
			want: "/",
		},
		{
			name: "normalizes_backslashes_and_repeated_slashes",
			in:   "\\\\{server_default}\\\\folder\\\\sub//",
			want: "/folder/sub/",
		},
		{
			name: "keeps_regular_paths_unchanged",
			in:   "/home/user/data/",
			want: "/home/user/data/",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeEndpointPath(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeEndpointPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetResponseUsesAbsoluteOffsetsWithHasNextPage(t *testing.T) {
	t.Parallel()

	offsets := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("offset was not an integer: %v", err)
		}
		offsets = append(offsets, offset)

		res := Response{
			Data: []Data{
				{
					Name: "dir-" + strconv.Itoa(offset),
					Type: "dir",
				},
			},
			HasNextPage: offset == 0,
			Limit:       100,
			Offset:      offset,
		}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	got, err := getResponse(context.Background(), server.URL+"?path=%2F", "token")
	if err != nil {
		t.Fatalf("getResponse returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("getResponse returned %d rows, want 2", len(got))
	}
	if !slices.Equal(offsets, []int{0, 100}) {
		t.Fatalf("offsets = %v, want [0 100]", offsets)
	}
}

func TestGetResponsePaginatesUsingTotalWhenHasNextPageMissing(t *testing.T) {
	t.Parallel()

	offsets := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("offset was not an integer: %v", err)
		}
		offsets = append(offsets, offset)

		count := 50
		if offset == 0 {
			count = 100
		}
		data := make([]Data, count)
		for i := 0; i < count; i++ {
			data[i] = Data{
				Name: "file-" + strconv.Itoa(offset+i),
				Type: "file",
			}
		}

		res := Response{
			Data:   data,
			Total:  150,
			Length: count,
			Limit:  100,
			Offset: offset,
		}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	got, err := getResponse(context.Background(), server.URL+"?path=%2F", "token")
	if err != nil {
		t.Fatalf("getResponse returned error: %v", err)
	}
	if len(got) != 150 {
		t.Fatalf("getResponse returned %d rows, want 150", len(got))
	}
	if !slices.Equal(offsets, []int{0, 100}) {
		t.Fatalf("offsets = %v, want [0 100]", offsets)
	}
}

func TestGetResponsePaginatesOnFullPagesWhenTotalMissing(t *testing.T) {
	t.Parallel()

	offsets := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("offset was not an integer: %v", err)
		}
		offsets = append(offsets, offset)

		count := 20
		if offset == 0 {
			count = 100
		}
		data := make([]Data, count)
		for i := 0; i < count; i++ {
			data[i] = Data{
				Name: "entry-" + strconv.Itoa(offset+i),
				Type: "file",
			}
		}

		res := Response{
			Data:   data,
			Length: count,
			Limit:  100,
			Offset: offset,
		}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	got, err := getResponse(context.Background(), server.URL+"?path=%2F", "token")
	if err != nil {
		t.Fatalf("getResponse returned error: %v", err)
	}
	if len(got) != 120 {
		t.Fatalf("getResponse returned %d rows, want 120", len(got))
	}
	if !slices.Equal(offsets, []int{0, 100}) {
		t.Fatalf("offsets = %v, want [0 100]", offsets)
	}
}

func TestGetResponseErrorsOnEmptyPageWithNext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := Response{HasNextPage: true, Data: []Data{}}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	_, err := getResponse(context.Background(), server.URL+"?path=%2F", "token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
