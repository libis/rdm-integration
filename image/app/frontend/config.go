// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package frontend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/utils"
	"net/http"
	"os"
)

//go:embed default_frontend_config.json
var configBytes []byte

var Config utils.Configuration

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
		utils.PluginConfig[v.Id] = v
	}
	utils.RedirectUri = Config.RedirectUri
}

func GetConfig(w http.ResponseWriter, r *http.Request) {
	if Config.ExternalURL == "" {
		Config.ExternalURL = utils.GetExternalDataverseURL()
		logging.Logger.Println(Config.ExternalURL)
	}
	b, err := json.Marshal(Config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
