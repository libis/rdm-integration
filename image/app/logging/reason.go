// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package logging

import (
	"fmt"
	"regexp"
	"strings"
)

// maxReasonLength cuts long details such as HTML error pages.
const maxReasonLength = 1000

// Query parameters that carry credentials: API keys, unblock keys and the
// signature of signed URLs.
var credentialR = regexp.MustCompile(`(?i)\b(key|unblock-key|apiToken|token|access_token)=[^&\s"'<>]+`)

// Reason writes what a remote service said about a failure to the server log.
// Clients only get fixed error messages, so the log is where a failure can
// still be diagnosed. Credentials in URLs are masked and long details are
// cut.
func Reason(what string, detail any) {
	var text string
	switch d := detail.(type) {
	case nil:
		text = ""
	case []byte:
		text = string(d)
	case error:
		text = d.Error()
	default:
		text = fmt.Sprint(d)
	}
	text = credentialR.ReplaceAllString(strings.TrimSpace(text), "$1=***")
	if len(text) > maxReasonLength {
		text = text[:maxReasonLength] + "..."
	}
	Logger.Printf("%s: %s", what, text)
}
