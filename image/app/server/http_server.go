// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package server

import (
	"crypto/tls"
	"integration/app/common"
	"integration/app/frontend"
	"integration/app/logging"
	"integration/app/plugin/funcs/compare"
	"integration/app/plugin/funcs/options"
	"integration/app/plugin/funcs/search"
	"integration/app/utils"
	"net/http"
	"time"
)

func Start() {
	srvMux := http.NewServeMux()

	// serve plugin api
	srvMux.HandleFunc("/api/plugin/compare", compare.Compare)
	srvMux.HandleFunc("/api/plugin/options", options.Options)
	srvMux.HandleFunc("/api/plugin/search", search.Search)

	// common
	srvMux.HandleFunc("/api/common/oauthtoken", common.GetOauthToken)
	srvMux.HandleFunc("/api/common/newdataset", common.NewDataset)
	srvMux.HandleFunc("/api/common/compare", common.Compare)
	srvMux.HandleFunc("/api/common/cached", common.GetCachedResponse)
	srvMux.HandleFunc("/api/common/store", common.Store)
	srvMux.HandleFunc("/api/common/dvobjects", common.DvObjects)

	// frontend config
	srvMux.HandleFunc("/api/frontend/config", frontend.GetConfig)

	// quit
	if utils.AllowQuit {
		srvMux.HandleFunc("/quit", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Server shut down and all jobs are cancelled. You can close the browser window now."))
			defer func() {
				logging.Logger.Println("quiting...")
				close(utils.Stop)
			}()
		})
	}

	// serve html
	srvMux.Handle("/", http.HandlerFunc(frontend.Frontend))

	// allow bad certificates
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	srv := &http.Server{
		Addr:              ":7788",
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 20 * time.Second,
		TLSConfig:         tlsConfig,
		Handler:           http.TimeoutHandler(srvMux, 50*time.Second, "processing the request took longer than 50 seconds: cancelled"),
	}
	srv.ListenAndServe()
}
