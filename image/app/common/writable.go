package common

import (
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
	"sort"
)

type WritableNodesRequest struct {
	SelectedNodes []*tree.Node `json:"selectedNodes"`
	OriginalRoot  tree.Node    `json:"originalRoot"`
}

type WritableNodesResponse struct {
	ToUpdate []string `json:"toUpdate"`
	ToDelete []string `json:"toDelete"`
	ToAdd    []string `json:"toAdd"`
}

func GetWritable(w http.ResponseWriter, r *http.Request) {
	req := WritableNodesRequest{}
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

	writableNodes := utils.ToWritableNodes(req.SelectedNodes, req.OriginalRoot)
	toUpdate := []string{}
	toDelete := []string{}
	toAdd := []string{}
	for _, v := range writableNodes {
		if v.Checked {
			if v.Attributes.LocalHash == "" {
				toAdd = append(toAdd, v.Id)
			} else {
				toUpdate = append(toUpdate, v.Id)
			}
		} else {
			toDelete = append(toDelete, v.Id)
		}
	}
	
	sort.Strings(toUpdate)
	sort.Strings(toDelete)
	sort.Strings(toAdd)

	res := WritableNodesResponse{
		ToUpdate: toUpdate,
		ToDelete: toDelete,
		ToAdd:    toAdd,
	}

	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}