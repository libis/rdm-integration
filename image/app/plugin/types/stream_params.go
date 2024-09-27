// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

type StreamParams struct {
	PluginId     string `json:"pluginId"`
	RepoName     string `json:"repoName"`
	Url          string `json:"url"`
	Option       string `json:"option"`
	User         string `json:"user"`
	Token        string `json:"token"`
	DVToken      string `json:"dvToken"`
	PersistentId string `json:"persistentId"`
}
