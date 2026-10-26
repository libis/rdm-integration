// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package search

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"integration/app/core/oauth"
	"integration/app/core/reauth"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

func Search(w http.ResponseWriter, r *http.Request) {
	//process request stream
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	params := types.OptionsRequest{}
	err = json.Unmarshal(b, &params)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}
	p, err := plugin.GetPlugin(params.Plugin)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("400 - " + err.Error()))
		return
	}

	params.Token = oauth.GetTokenFromCache(r.Context(), params.Token, params.Token, params.PluginId)
	if params.User == "" {
		params.User = core.GetUserFromHeader(r.Header)
	}
	res, err := p.Search(r.Context(), params)
	if err != nil {
		reauth.WriteError(w, err)
		return
	}
	if len(res) == 0 {
		res = append(res, types.SelectItem{Label: "no results found for \"" + params.RepoName + "\"", Value: "empty"})
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
