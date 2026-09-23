// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core/reauth"
	"integration/app/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestCachedCompareCanBeReadAgainWithoutExtendingExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := testutil.NewFakeRedis()
		config.SetRedis(fr)
		defer fr.Reset()
		cached := CachedResponse{Key: "compare-result"}
		cached.Response.Id = "doi:10.1/TEST"
		CacheResponse(cached)

		read := func() CachedResponse {
			t.Helper()
			rec := httptest.NewRecorder()
			GetCachedResponse(rec, httptest.NewRequest(http.MethodPost, "/api/common/cached", strings.NewReader(`{"key":"compare-result"}`)))
			if rec.Code != http.StatusOK {
				t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
			}
			var res CachedResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatal(err)
			}
			return res
		}
		for i := 0; i < 2; i++ {
			res := read()
			if !res.Ready || res.Response.Id != cached.Response.Id {
				t.Fatalf("read %d lost the completed result: %+v", i+1, res)
			}
			if i == 0 {
				time.Sleep(time.Minute)
			}
		}
		time.Sleep(cacheMaxDuration - time.Minute + time.Second)
		if read().Ready {
			t.Error("reading the result extended its expiry")
		}
	})
}

// A failed job leaves its error under "error <persistentId>". The Compare
// handler must consume that key when reporting it: a reauth-marked error that
// stays cached would keep answering 401 for its whole TTL, sending the user
// back to the OAuth provider on every retry even after they re-authenticated.
func TestCompareConsumesCachedJobErrorOnRead(t *testing.T) {
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	defer fr.Reset()

	persistentId := "doi:10.1/TEST"
	errorKey := fmt.Sprintf("error %v", persistentId)
	reauthErr := &reauth.Error{RequiredScopes: []string{"urn:globus:auth:scope:transfer.api.globus.org:all"}}
	fr.Set(context.Background(), errorKey, reauthErr.Error(), time.Minute)

	body := fmt.Sprintf(`{"persistentId": %q, "data": []}`, persistentId)
	first := httptest.NewRecorder()
	Compare(first, httptest.NewRequest(http.MethodPost, "/api/common/compare", strings.NewReader(body)))

	if first.Code != http.StatusUnauthorized {
		t.Errorf("expected the cached reauth error to produce a 401, got %v: %v", first.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), "reauth") {
		t.Errorf("expected a structured reauth body, got: %v", first.Body.String())
	}
	if remaining := fr.Get(context.Background(), errorKey).Val(); remaining != "" {
		t.Errorf("expected the cached job error to be consumed by the read, but it is still cached: %v", remaining)
	}
}
