package main

import (
	"crypto/tls"
	"integration/app/common"
	"integration/app/gh"
	"integration/app/utils"
	"net/http"
	"strings"
)

func main() {
	// allow bad certificates
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// serve api
	// github
	http.HandleFunc("/api/github/compare", gh.GithubCompare)
	http.HandleFunc("/api/github/store", gh.GithubStore)
	//common
	http.HandleFunc("/api/common/newdataset", common.NewDataset)
	http.HandleFunc("/api/common/compare", common.Compare)
	http.HandleFunc("/api/common/cached", common.GetCachedResponse)

	// serve html
	fs := http.FileServer(http.Dir(utils.FileServerPath))
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/connect") {
			r.URL.Path = "/"
		}
		fs.ServeHTTP(w, r)
	}))
	http.ListenAndServe(":7788", nil)
}
