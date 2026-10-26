// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"os"
	"slices"
)

//go:embed default_frontend_config.json
var defaultFrontendConfig []byte

type RepoPlugin struct {
	Id                        string      `json:"id"`
	Name                      string      `json:"name"`
	Plugin                    string      `json:"plugin"`
	PluginName                string      `json:"pluginName"`
	OptionFieldName           string      `json:"optionFieldName,omitempty"`
	OptionPlaceholder         string      `json:"optionFieldPlaceholder,omitempty"`
	OptionFieldInteractive    bool        `json:"optionFieldInteractive,omitempty"`
	TokenFieldName            string      `json:"tokenFieldName,omitempty"`
	TokenFieldPlaceholder     string      `json:"tokenFieldPlaceholder,omitempty"`
	SourceUrlFieldName        string      `json:"sourceUrlFieldName,omitempty"`
	SourceUrlFieldPlaceholder string      `json:"sourceUrlFieldPlaceholder,omitempty"`
	SourceUrlFieldValue       string      `json:"sourceUrlFieldValue,omitempty"`
	UsernameFieldName         string      `json:"usernameFieldName,omitempty"`
	UsernameFieldPlaceholder  string      `json:"usernameFieldPlaceholder,omitempty"`
	RepoNameFieldName         string      `json:"repoNameFieldName,omitempty"`
	RepoNameFieldPlaceholder  string      `json:"repoNameFieldPlaceholder,omitempty"`
	RepoNameFieldEditable     bool        `json:"repoNameFieldEditable,omitempty"`
	RepoNameFieldValues       []string    `json:"repoNameFieldValues,omitempty"`
	RepoNameFieldHasSearch    bool        `json:"repoNameFieldHasSearch"`
	RepoNameFieldHasInit      bool        `json:"repoNameFieldHasInit"`
	ParseSourceUrlField       bool        `json:"parseSourceUrlField"`
	TokenName                 string      `json:"tokenName,omitempty"`
	TokenGetter               TokenGetter `json:"tokenGetter,omitempty"`
}

type TokenGetter struct {
	Url                         string   `json:"URL,omitempty"`
	OauthClientId               string   `json:"oauth_client_id,omitempty"`
	Scopes                      []string `json:"scopes,omitempty"`
	SessionRequiredSingleDomain []string `json:"session_required_single_domain,omitempty"`
}

type Configuration struct {
	DataverseHeader         string       `json:"dataverseHeader"`
	CollectionOptionsHidden bool         `json:"collectionOptionsHidden"`
	CreateNewDatasetEnabled bool         `json:"createNewDatasetEnabled"`
	DatasetFieldEditable    bool         `json:"datasetFieldEditable"`
	CollectionFieldEditable bool         `json:"collectionFieldEditable"`
	ExternalURL             string       `json:"externalURL"`
	ShowDvTokenGetter       bool         `json:"showDvTokenGetter"`
	ShowDvToken             bool         `json:"showDvToken"`
	RedirectUri             string       `json:"redirect_uri,omitempty"`
	LoginRedirectUrl        string       `json:"loginRedirectUrl,omitempty"`
	SendMails               bool         `json:"sendMails"`
	Plugins                 []RepoPlugin `json:"plugins"`
	Queues                  []Queue      `json:"queues"`
}

type Queue struct {
	Label          string   `json:"label"`
	Value          string   `json:"value"`
	FileExtensions []string `json:"fileExtensions"`
}

// LoadFrontendConfig reads the frontend configuration from the file named by
// FRONTEND_CONFIG_FILE, or uses the embedded default when that file cannot be
// read.
func LoadFrontendConfig() Configuration {
	b := defaultFrontendConfig
	configFile := os.Getenv("FRONTEND_CONFIG_FILE")
	if fromFile, err := os.ReadFile(configFile); err == nil {
		logging.Logger.Printf("using frontend configuration from %v\n", configFile)
		b = fromFile
	}
	res := Configuration{}
	if err := json.Unmarshal(b, &res); err != nil {
		panic(fmt.Errorf("frontend configuration could not be loaded"))
	}
	return res
}

// PluginTypes returns the plugin type of each configured plugin, once, in
// configuration order. The backend serves only these plugin types.
func (c Configuration) PluginTypes() []string {
	res := []string{}
	for _, p := range c.Plugins {
		if p.Plugin != "" && !slices.Contains(res, p.Plugin) {
			res = append(res, p.Plugin)
		}
	}
	return res
}
