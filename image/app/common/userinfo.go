// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"encoding/json"
	"integration/app/core"
	"net/http"
)

type UserInfoResponse struct {
	LoggedIn bool `json:"loggedIn"`
}

func GetUserInfo(w http.ResponseWriter, r *http.Request) {
	user := core.GetUserFromHeader(r.Header)
	res := UserInfoResponse{
		LoggedIn: user != "",
	}
	b, err := json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - internal error"))
		return
	}
	w.Write(b)
}
