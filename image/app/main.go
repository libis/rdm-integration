package main

import (
	"crypto/tls"
	"embed"
	"integration/app/common"
	"integration/app/gh"
	"io/fs"
	"net/http"
)

//go:embed html
var staticFiles embed.FS

func main() {
	// allow bad certificates
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	
	// serve api
	// github
	http.HandleFunc("/api/github/tree", gh.GithubTree)
	http.HandleFunc("/api/github/store", gh.GithubStore)
	//common
	http.HandleFunc("/api/common/writable", common.GetWritable)

	// serve html
	fs := http.FileServer(http.FS(fs.FS(staticFiles)))
	http.Handle("/", fs)
	http.ListenAndServe(":7788", nil)
}
