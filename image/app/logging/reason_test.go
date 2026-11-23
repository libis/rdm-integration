// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package logging

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := Logger
	Logger = log.New(buf, "", 0)
	t.Cleanup(func() { Logger = prev })
	return buf
}

func TestReasonLogsWhatTheServiceSaid(t *testing.T) {
	buf := captureLog(t)
	Reason("adding file failed", "file exceeds the size limit")
	if got := buf.String(); got != "adding file failed: file exceeds the size limit\n" {
		t.Fatalf("log = %q", got)
	}
}

func TestReasonMasksCredentialsInURLs(t *testing.T) {
	buf := captureLog(t)
	Reason("listing Dataverse files failed", errors.New(
		`Get "https://dv.example/api/v1/datasets/:persistentId?persistentId=doi:1&key=abc-123": dial tcp: refused`+
			` signed https://dv.example/api/files/1?until=2026&user=u1&method=GET&token=f00d&unblock-key=s3cret&apiToken=t0k`))
	got := buf.String()
	for _, secret := range []string{"abc-123", "f00d", "s3cret", "t0k"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log leaks %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "key=***") || !strings.Contains(got, "persistentId=doi:1") {
		t.Fatalf("log lost the useful parts: %s", got)
	}
}

func TestReasonCutsLongBodies(t *testing.T) {
	buf := captureLog(t)
	Reason("listing Dataverse objects failed", []byte("<html>"+strings.Repeat("x", 5000)+"</html>"))
	got := strings.TrimSuffix(buf.String(), "\n")
	if !strings.HasPrefix(got, "listing Dataverse objects failed: <html>xxx") {
		t.Fatalf("log = %.80q", got)
	}
	if len(got) > 1100 || !strings.HasSuffix(got, "...") {
		t.Fatalf("log not cut: %d characters", len(got))
	}
}
