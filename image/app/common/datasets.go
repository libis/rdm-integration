package common

import (
	"encoding/json"
	"fmt"
	"integration/app/utils"
	"io"
	"net/http"
)

func Datasets(w http.ResponseWriter, r *http.Request) {
	//process request
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	token := Token{}
	err = json.Unmarshal(b, &token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	datasets, err := utils.ListDatasets(token.Token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	res := []SelectItem{}
	added := map[string]bool{}
	for _, v := range datasets {
		label := v.Name + " (" + v.GlobalId + ")"
		if !added[label] {
			added[label] = true
			res = append(res, SelectItem{
				Label: v.Name + " (" + v.GlobalId + ")",
				Value: v.GlobalId,
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
