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
	OptionFieldName           string `json:"optionFieldName,omitempty"`
	OptionPlaceholder         string `json:"optionFieldPlaceholder,omitempty"`
	TokenFieldName            string `json:"tokenFieldName,omitempty"`
	TokenFieldPlaceholder     string `json:"tokenFieldPlaceholder,omitempty"`
	SourceUrlFieldName        string `json:"sourceUrlFieldName"`
	SourceUrlFieldPlaceholder string `json:"sourceUrlFieldPlaceholder"`
	UsernameFieldName         string `json:"usernameFieldName,omitempty"`
	UsernameFieldPlaceholder  string `json:"usernameFieldPlaceholder,omitempty"`
	ZoneFieldName             string `json:"zoneFieldName,omitempty"`
	ZoneFieldPlaceholder      string `json:"zoneFieldPlaceholder,omitempty"`
	ParseSourceUrlField       bool   `json:"parseSourceUrlField"`
	TokenName                 string `json:"tokenName,omitempty"`
}
type Configuration struct {
	DataverseHeader         string       `json:"dataverseHeader"`
	CollectionOptionsHidden bool         `json:"collectionOptionsHidden"`
	CreateNewDatasetEnabled bool         `json:"createNewDatasetEnabled"`
	DatasetFieldEditable    bool         `json:"datasetFieldEditable"`
	CollectionFieldEditable bool         `json:"collectionFieldEditable"`
	Plugins                 []RepoPlugin `json:"plugins"`
}

//go:embed default_frontend_config.json
var configBytes []byte

var Config Configuration

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
}

func GetConfig(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(Config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
