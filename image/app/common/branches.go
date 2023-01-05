package common

import (
	"encoding/json"
	"fmt"
	"integration/app/utils"
	"io"
	"net/http"
)

func Branches(w http.ResponseWriter, r *http.Request) {
	//process request
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	params := map[string]string{}
	err = json.Unmarshal(b, &params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	branches := []string{}
	if params["repoType"] == "github" {
		branches, err = utils.GithubBranches(params)
	} else if params["repoType"] == "gitlab" {
		branches, err = utils.GitlabBranches(params)
	} else if params["repoType"] == "irods" {
		branches, err = utils.IrodsFolders(params)
	} else {
		err = fmt.Errorf("unknown repoType: " + params["repoType"])
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	res := []SelectItem{}
	for _, v := range branches {
		res = append(res, SelectItem{
			Label: v,
			Value: v,
		})
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
