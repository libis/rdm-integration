// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

type RepoPlugin struct {
	Id                        string      `json:"id"`
	Plugin                    string      `json:"plugin"`
	Name                      string      `json:"name"`
	OptionFieldName           string      `json:"optionFieldName,omitempty"`
	OptionPlaceholder         string      `json:"optionFieldPlaceholder,omitempty"`
	TokenFieldName            string      `json:"tokenFieldName,omitempty"`
	TokenFieldPlaceholder     string      `json:"tokenFieldPlaceholder,omitempty"`
	SourceUrlFieldName        string      `json:"sourceUrlFieldName,omitempty"`
	SourceUrlFieldPlaceholder string      `json:"sourceUrlFieldPlaceholder,omitempty"`
	SourceUrlFieldValue       string      `json:"sourceUrlFieldValue,omitempty"`
	UsernameFieldName         string      `json:"usernameFieldName,omitempty"`
	UsernameFieldPlaceholder  string      `json:"usernameFieldPlaceholder,omitempty"`
	ZoneFieldName             string      `json:"zoneFieldName,omitempty"`
	ZoneFieldPlaceholder      string      `json:"zoneFieldPlaceholder,omitempty"`
	ZoneFieldEditable         bool        `json:"zoneFieldEditable,omitempty"`
	ZoneFieldValues           []string    `json:"zoneFieldValues,omitempty"`
	ParseSourceUrlField       bool        `json:"parseSourceUrlField"`
	TokenName                 string      `json:"tokenName,omitempty"`
	TokenGetter               TokenGetter `json:"tokenGetter,omitempty"`
}

type TokenGetter struct {
	Url           string `json:"URL,omitempty"`
	OauthClientId string `json:"oauth_client_id,omitempty"`
}

type Configuration struct {
	DataverseHeader         string       `json:"dataverseHeader"`
	CollectionOptionsHidden bool         `json:"collectionOptionsHidden"`
	CreateNewDatasetEnabled bool         `json:"createNewDatasetEnabled"`
	DatasetFieldEditable    bool         `json:"datasetFieldEditable"`
	CollectionFieldEditable bool         `json:"collectionFieldEditable"`
	ExternalURL             string       `json:"externalURL"`
	ShowDvTokenGetter       bool         `json:"showDvTokenGetter"`
	RedirectUri             string       `json:"redirect_uri,omitempty"`
	Plugins                 []RepoPlugin `json:"plugins"`
}
