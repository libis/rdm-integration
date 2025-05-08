package common

import (
	"integration/app/core"
	"io"
	"net/http"
)

func GetUserEmail(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}
	r.Body.Close()
	user := core.GetUserFromHeader(r.Header)
	to, err := core.Destination.GetUserEmail(r.Context(), string(b), user)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - user not found"))
		return
	}

	w.Write([]byte(to))
}
