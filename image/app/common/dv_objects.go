// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"io"
	"net/http"
)

type DvObjectsRequest struct {
	Token      string `json:"token"`
	Collection string `json:"collectionId"`
	ObjectType string `json:"objectType"`
	SearchTerm string `json:"searchTerm"`
}

func DvObjects(w http.ResponseWriter, r *http.Request) {
	user := core.GetUserFromHeader(r.Header)
	//process request
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	req := DvObjectsRequest{}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	res, err := core.Destination.Options(r.Context(), req.ObjectType, req.Collection, req.SearchTerm, req.Token, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	if err != nil {
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
