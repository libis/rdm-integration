// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package frontend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core/oauth"
	"integration/app/logging"
	"net/http"
	"os"
	"strings"
)

//go:embed default_frontend_config.json
var configBytes []byte

var Config config.Configuration

func init() {
	// read configuration
	configFile := os.Getenv("FRONTEND_CONFIG_FILE")
	b, err := os.ReadFile(configFile)
	if err == nil {
		logging.Logger.Printf("using frontend configuration from %v\n", configFile)
		configBytes = b
	}
	err = json.Unmarshal(configBytes, &Config)
	if err != nil {
		panic(fmt.Errorf("could not unmarshal config: %v", err))
	}
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
		Config.LoginRedirectUrl = config.GetConfig().Options.LoginRedirectUrl
		Config.GlobusGuestDownloadEnabled = config.GetConfig().Options.GlobusGuestDownloadUserName != ""
	}
	b, err := json.Marshal(Config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
