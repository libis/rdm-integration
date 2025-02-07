// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package frontend

import (
	"embed"
	"net/http"
	"strings"
)

// content is our static web server content.
//
//go:embed all:dist/datasync
var content embed.FS

var fs http.Handler = http.FileServer(http.FS(content))

func init() {}

func Frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/connect") || strings.HasPrefix(r.URL.Path, "/connect/") || r.URL.Path == "" {
		url := strings.ReplaceAll(Config.RedirectUri, "/connect", "/#/connect")
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			url += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	} else {
		r.URL.Path = "/dist/datasync" + r.URL.Path
		fs.ServeHTTP(w, r)
	}
}
