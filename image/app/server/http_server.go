// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package server

import (
	"crypto/tls"
	"fmt"
	"integration/app/common"
	"integration/app/config"
	"integration/app/core"
	"integration/app/frontend"
	"integration/app/logging"
	"integration/app/plugin/funcs/compare"
	"integration/app/plugin/funcs/options"
	"integration/app/plugin/funcs/search"
	"net/http"
	"time"
)

const timeout = 5 * time.Minute

func Start() {
	srvMux := http.NewServeMux()

	// serve plugin api
	srvMux.HandleFunc("/api/plugin/compare", compare.Compare)
	srvMux.HandleFunc("/api/plugin/options", options.Options)
	srvMux.HandleFunc("/api/plugin/search", search.Search)

	// common
	srvMux.HandleFunc("/api/common/oauthtoken", common.GetOauthToken)
	srvMux.HandleFunc("/api/common/newdataset", common.NewDataset)
	srvMux.HandleFunc("/api/common/datasetversion", common.GetDatasetVersion)
	srvMux.HandleFunc("/api/common/compare", common.Compare)
	srvMux.HandleFunc("/api/common/cached", common.GetCachedResponse)
	srvMux.HandleFunc("/api/common/store", common.Store)
	srvMux.HandleFunc("/api/common/dvobjects", common.DvObjects)
	srvMux.HandleFunc("/api/common/executable", common.GetExecutableFiles)
	srvMux.HandleFunc("/api/common/checkaccess", common.GetAccessToQueue)
	srvMux.HandleFunc("/api/common/compute", common.Compute)
	srvMux.HandleFunc("/api/common/cachedcompute", common.GetCachedComputeResponse)
	srvMux.HandleFunc("/api/common/ddicdi", common.DdiCdi)
	srvMux.HandleFunc("/api/common/cachedddicdi", common.GetCachedDdiCdiResponse)
	srvMux.HandleFunc("/api/common/cachedddicdioutput", common.GetCachedDdiCdiOutput)
	srvMux.HandleFunc("/api/common/ddicdicompatible", common.GetDdiCdiCompatibleFiles)
	srvMux.HandleFunc("/api/common/addfile", common.AddFileToDataset)
	srvMux.HandleFunc("/api/common/downloadable", common.GetDownloadableFiles)
	srvMux.HandleFunc("/api/common/download", common.Download)
	srvMux.HandleFunc("/api/common/globus/status", common.GlobusTransferStatus)
	srvMux.HandleFunc("/api/common/metadata", common.GetMetadata)
	srvMux.HandleFunc("/api/common/useremail", common.GetUserEmail)

	// frontend config
	srvMux.HandleFunc("/api/frontend/config", frontend.GetConfig)
	srvMux.HandleFunc("/api/frontend/shacl", frontend.GetShaclShapes)

	// quit
	if config.AllowQuit {
		srvMux.HandleFunc("/quit", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Server shut down and all jobs are cancelled. You can close the browser window now."))
			defer func() {
				logging.Logger.Println("quitting...")
				close(core.Stop)
			}()
		})
	}

	// serve html
	srvMux.Handle("/", http.HandlerFunc(frontend.Frontend))

	// allow bad certificates
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	srv := &http.Server{
		Addr:              ":7788",
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       timeout,
		ReadHeaderTimeout: timeout,
		TLSConfig:         tlsConfig,
		Handler:           http.TimeoutHandler(srvMux, timeout, fmt.Sprintf("processing the request took longer than %v: cancelled", timeout)),
	}
	srv.ListenAndServe()
}
