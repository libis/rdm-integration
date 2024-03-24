// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"io"
	"net/http"
)

type AccessRequest struct {
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
	Queue        string `json:"queue"`
}

type AccessResponse struct {
	Access  bool   `json:"access"`
	Message string `json:"message"`
}

// this is called when polling for status changes, after specific compare is finished or store is calleed
func GetAccessToQueue(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := AccessRequest{}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	res, err := checkAccess(req, r)

	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func checkAccess(req AccessRequest, r *http.Request) (AccessResponse, error) {
	res := AccessResponse{}
	if config.GetConfig().Options.ComputationAccessEndpoint != "" {
		// TODO: access endpoint api at Dataverse?
		res.Access = false
		res.Message = "access endpoint not implemented"
	} else {
		username := core.GetUserFromHeader(r.Header)
		email, err := core.Destination.GetUserEmail(r.Context(), req.DataverseKey, username)
		if err != nil {
			return res, err
		}
		res.Access = config.HasAccessToQueue(email, req.Queue)
		if !res.Access {
			res.Message = "Access denied!"
		}
	}
	return res, nil
}
