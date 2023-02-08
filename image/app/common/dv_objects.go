// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/utils"
	"io"
	"net/http"
)

type DvObjectsRequest struct {
	Token      string `json:"token"`
	Collection string `json:"collectionId"`
	ObjectType string `json:"objectType"`
}

func DvObjects(w http.ResponseWriter, r *http.Request) {
	user := utils.GetUserFromHeader(r.Header)
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

	dvObjects, err := utils.ListDvObjects(r.Context(), req.ObjectType, req.Collection, req.Token, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	res := []types.SelectItem{}
	added := map[string]bool{}
	for _, v := range dvObjects {
		id := v.GlobalId
		if id == "" {
			id = v.Identifier
		}
		label := v.Name + " (" + id + ")"
		if !added[label] {
			added[label] = true
			res = append(res, types.SelectItem{
				Label: label,
				Value: id,
			})
		}
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
