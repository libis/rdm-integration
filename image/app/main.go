package main

import (
	"crypto/tls"
	"embed"
	"integration/app/gh"
	"io/fs"
	"net/http"
)

//go:embed html
var staticFiles embed.FS

func main() {
	// serve api
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.HandleFunc("/api/github/tree", gh.GithubTree)
	http.HandleFunc("/api/github/store", gh.GithubStore)

	// serve html
	fs := http.FileServer(http.FS(fs.FS(staticFiles)))
	http.Handle("/", fs)
	http.ListenAndServe(":7788", nil)
}
