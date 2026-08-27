// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"integration/app/logging"
	"io"
	"net/http"
)

type DvObjectsRequest struct {
	Token       string `json:"token"`
	Collection  string `json:"collectionId"`
	ObjectType  string `json:"objectType"`
	SearchTerm  string `json:"searchTerm"`
	ForDownload bool   `json:"forDownload"`
}

func DvObjects(w http.ResponseWriter, r *http.Request) {
	user := core.GetUserFromHeader(r.Header)
	//process request
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	req := DvObjectsRequest{}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	// Use DownloadableOptions for download context (includes public datasets)
	// Use Options for upload context (only user's datasets with write access)
	var res interface{}
	if req.ForDownload && core.Destination.DownloadableOptions != nil {
		res, err = core.Destination.DownloadableOptions(r.Context(), req.ObjectType, req.Collection, req.SearchTerm, req.Token, user)
	} else {
		res, err = core.Destination.Options(r.Context(), req.ObjectType, req.Collection, req.SearchTerm, req.Token, user)
	}
	if err != nil {
		// Log the failure: the reverse proxy (ProxyErrorOverride) replaces 500
		// bodies with a generic page, so this is the only place the cause survives.
		logging.Logger.Printf("dvobjects failed for user %v (type %v, search %q): %v", user, req.ObjectType, req.SearchTerm, err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
