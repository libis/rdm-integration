package common

import (
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
)

type CompareRequest struct {
	Data         []tree.Node `json:"data"`
	PersistentId string      `json:"persistentId"`
}

func Compare(w http.ResponseWriter, r *http.Request) {
	//process request
	req := CompareRequest{}
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	//map nodes
	nm := map[string]tree.Node{}
	for _, v := range req.Data {
		nm[v.Id] = v
	}

	//compare and write response
	res := utils.Compare(nm, req.PersistentId, "")
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
