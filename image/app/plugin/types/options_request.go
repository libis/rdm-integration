// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

type OptionsRequest struct {
	PluginId string `json:"pluginId"`
	Plugin   string `json:"plugin"`
	RepoName string `json:"repoName"`
	Option   string `json:"option"`
	Url      string `json:"url"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
