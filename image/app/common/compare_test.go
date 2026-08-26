// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package common

import (
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/core/reauth"
	"integration/app/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
