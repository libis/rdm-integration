// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

type CompareRequest struct {
	PluginId     string `json:"pluginId"`
	Plugin       string `json:"plugin"`
	RepoName     string `json:"repoName"`
	Url          string `json:"url"`
	Option       string `json:"option"`
	User         string `json:"user"`
	Token        string `json:"token"`
	PersistentId string `json:"persistentId"`
	NewlyCreated bool   `json:"newlyCreated"`
	DataverseKey string `json:"dataverseKey"`
	SessionId    string `json:"sessionId"`
}
