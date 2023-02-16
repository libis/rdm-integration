// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"integration/app/config"
	"net/http"
)

func GetUserFromHeader(h http.Header) string {
	hn := "Ajp_uid"
	if config.GetConfig().Options.UserHeaderName != "" {
		hn = config.GetConfig().Options.UserHeaderName
	}
	return h.Get(hn)
}
