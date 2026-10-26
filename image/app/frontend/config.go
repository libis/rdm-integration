// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package frontend

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core/oauth"
	"integration/app/logging"
	"net/http"
	"strings"
)

var Config config.Configuration

func init() {
	Config = config.LoadFrontendConfig()
	for _, v := range Config.Plugins {
		oauth.PluginConfig[v.Id] = v
	}
	oauth.RedirectUri = Config.RedirectUri
}

func GetConfig(w http.ResponseWriter, r *http.Request) {
	if Config.ExternalURL == "" {
		Config.ExternalURL = config.GetExternalDestinationURL()
		logging.Logger.Println(Config.ExternalURL)
		if strings.Contains(Config.ExternalURL, "kuleuven") {
			Config.CollectionOptionsHidden = true
		}
		Config.Queues = config.GetComputationQueues()
	}
	b, err := json.Marshal(Config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
