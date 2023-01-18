package server

import (
	"crypto/tls"
	"integration/app/common"
	"integration/app/frontend"
	"integration/app/logging"
	"integration/app/plugin/funcs/compare"
	"integration/app/plugin/funcs/options"
	"integration/app/utils"
	"net/http"
)

func Start() {
	// allow bad certificates
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// serve plugin api
	http.HandleFunc("/api/plugin/compare", compare.Compare)
	http.HandleFunc("/api/plugin/options", options.Options)

	// common
	http.HandleFunc("/api/common/newdataset", common.NewDataset)
	http.HandleFunc("/api/common/compare", common.Compare)
	http.HandleFunc("/api/common/cached", common.GetCachedResponse)
	http.HandleFunc("/api/common/store", common.Store)
	http.HandleFunc("/api/common/dvobjects", common.DvObjects)

	// frontend config
	http.HandleFunc("/api/frontend/config", frontend.Config)

	// quit
	if utils.AllowQuit {
		http.HandleFunc("/quit", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Server shut down and all jobs are cancelled. You can close the browser window now."))
			defer func() {
				logging.Logger.Println("quiting...")
				close(utils.Stop)
			}()
		})
	}

	// serve html
	http.Handle("/", http.HandlerFunc(frontend.Frontend))

	http.ListenAndServe(":7788", nil)
}
