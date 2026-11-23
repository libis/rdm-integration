// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"bytes"
	"integration/app/logging"
	"log"
	"testing"
)

// captureLog collects the server log for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := logging.Logger
	logging.Logger = log.New(buf, "", 0)
	t.Cleanup(func() { logging.Logger = prev })
	return buf
}
