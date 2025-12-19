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

	// Handle legacy hash-based URLs: redirect /#/path to /path
	if strings.HasPrefix(r.URL.Path, "/#/") {
		newPath := strings.TrimPrefix(r.URL.Path, "/#")
		if r.URL.RawQuery != "" {
			newPath += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, newPath, http.StatusMovedPermanently)
		return
	}

	// Try to serve static file first
	staticPath := "/dist/datasync" + r.URL.Path
	if file, err := content.Open(staticPath[1:]); err == nil {
		file.Close()
		// File exists, serve it
		r.URL.Path = staticPath
		fs.ServeHTTP(w, r)
		return
	}

	// File doesn't exist - serve index.html for Angular routing
	// This handles /connect, /download, /ddi-cdi, etc.
	indexFile, err := content.ReadFile("dist/datasync/index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexFile)
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
