package globus

import "testing"

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
