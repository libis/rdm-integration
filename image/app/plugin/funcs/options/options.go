// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package options

import (
	"encoding/json"
	"fmt"
	"integration/app/core"
	"integration/app/plugin"
	"integration/app/plugin/types"
	"io"
	"net/http"
)

func Options(w http.ResponseWriter, r *http.Request) {
	//process requeststream
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

	sessionId := core.GetShibSessionFromHeader(r.Header)
	params.Token, _ = core.GetTokenFromCache(r.Context(), params.Token, sessionId, params.PluginId)
	if params.User == "" {
		params.User = core.GetUserFromHeader(r.Header)
	}
	res, err := plugin.GetPlugin(params.Plugin).Options(r.Context(), params)
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
