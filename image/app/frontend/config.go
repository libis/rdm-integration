package frontend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"net/http"
	"os"
)

type RepoPlugin struct {
	Id                        string `json:"id"`
	Name                      string `json:"name"`
	OptionFieldName           string `json:"optionFieldName"`
	TokenFieldName            string `json:"tokenFieldName"`
	SourceUrlFieldPlaceholder string `json:"sourceUrlFieldPlaceholder"`
	TokenFieldPlaceholder     string `json:"tokenFieldPlaceholder"`
	UsernameFieldHidden       bool   `json:"usernameFieldHidden"`
	ZoneFieldHidden           bool   `json:"zoneFieldHidden"`
	ParseSourceUrlField       bool   `json:"parseSourceUrlField"`
	TokenName                 string `json:"tokenName,omitempty"`
}
type Config struct {
	DataverseHeader         string       `json:"dataverseHeader"`
	CollectionOptionsHidden bool         `json:"collectionOptionsHidden"`
	Plugins                 []RepoPlugin `json:"plugins"`
}

//go:embed default_frontend_config.json
var configBytes []byte

var config Config

func init() {
	// read configuration
	configFile := os.Getenv("FRONTEND_CONFIG_FILE")
	b, err := os.ReadFile(configFile)
	if err != nil {
		logging.Logger.Printf("config file %v not found: using default frontend config file\n", configFile)
	} else {
		configBytes = b
	}
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		panic(fmt.Errorf("could not unmarshal config: %v", err))
	}
}

func GetConfig(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
