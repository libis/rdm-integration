// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package frontend

import (
	"embed"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// content is our static web server content.
//
//go:embed all:dist/datasync
var content embed.FS

var (
	fs       http.Handler = http.FileServer(http.FS(content))
	devMode               = strings.EqualFold(os.Getenv("DEV"), "true")
	devProxy *httputil.ReverseProxy
)

func init() {
	if devMode {
		setupDevProxy()
	}
}

func Frontend(w http.ResponseWriter, r *http.Request) {
	if devProxy != nil {
		devProxy.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/connect") || strings.HasPrefix(r.URL.Path, "/connect/") || r.URL.Path == "" {
		url := strings.ReplaceAll(Config.RedirectUri, "/connect", "/#/connect")
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			url += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	} else if strings.HasPrefix(r.URL.Path, "/download") || strings.HasPrefix(r.URL.Path, "/download/") {
		url := strings.ReplaceAll(Config.RedirectUri, "/connect", "/#/download")
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			url += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	} else {
		r.URL.Path = "/dist/datasync" + r.URL.Path
		fs.ServeHTTP(w, r)
	}
}

func setupDevProxy() {
	target := "http://127.0.0.1:4200"
	parsed, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("frontend: dev proxy error: %v", err)
		http.Error(w, "Frontend dev server unavailable", http.StatusBadGateway)
	}
	devProxy = proxy
	log.Printf("frontend: proxying UI traffic to %s", parsed.String())
}
