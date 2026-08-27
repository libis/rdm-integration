// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/libis/rdm-dataverse-go-api/api"
)

// Guard test for the rdm-dataverse-go-api dependency: the signed-URL flow must
// validate against a Dataverse that, like every version up to 6.7.x deployed at
// KU Leuven, URL-decodes the request URI before checking the signature. This
// compiles against whatever go-api version the module system actually resolves,
// so a bad release (e.g. the module-proxy copy of v1.0.9, which signs the
// still-encoded URL and breaks every percent-encoded query) fails the build's
// tests instead of surfacing as "Bad signed URL" 500s in production search.

const guardSigningKey = "guard-signing-key"

func guardSha512(s string) string {
	h := sha512.Sum512([]byte(s))
	return hex.EncodeToString(h[:])
}

func guardSignLikeDataverse(u string) string {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	toSign := u + sep + "until=2999-01-01T00:00:00.000&user=alice&method=GET&token="
	return toSign + guardSha512(toSign+guardSigningKey)
}

func guardValidates(raw string) bool {
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return false
	}
	i := strings.LastIndex(decoded, "token=")
	if i < 0 {
		return false
	}
	return decoded[i+len("token="):] == guardSha512(decoded[:i+len("token=")]+guardSigningKey)
}

func TestSignedMyDataSearchValidatesOnDecodedOnlyDataverse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/requestSignedUrl") {
			req := api.SigningRequest{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("bad signing request: %v", err)
			}
			resp := api.SignedUrlResponse{}
			resp.Status = "OK"
			resp.Data.SignedUrl = guardSignLikeDataverse(req.Url)
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encoding signing response: %v", err)
			}
			return
		}
		// Validate like Dataverse <= 6.7.x: only against the URL-decoded URI.
		if !guardValidates("http://" + r.Host + r.RequestURI) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"status":"ERROR","message":"Bad signed URL"}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"pagination":{"hasNextPageNumber":false},"items":[]}}`)
	}))
	defer ts.Close()

	client := api.NewUrlSigningClient(ts.URL, "alice", "admin-api-key", "unblock")
	path := "/api/v1/mydata/retrieve?selected_page=1&dvobject_types=Dataset" +
		"&role_ids=6&role_ids=7&mydata_search_term=" + url.QueryEscape(`text:"Replication Data"`)
	req := client.NewRequest(path, "GET", nil, nil)
	res := api.RetrieveResponse{}
	if err := api.Do(context.Background(), req, &res); err != nil {
		t.Fatalf("signed mydata search failed: %v", err)
	}
	if !res.Success {
		t.Errorf("signed mydata search with a percent-encoded term must validate, got: %v", res.ErrorMessage)
	}
}
