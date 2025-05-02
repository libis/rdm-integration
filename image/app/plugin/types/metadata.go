package types

const NewDataset = "New Dataset"

type Metadata map[string]interface{}

type GetMetadataRequest struct {
	PluginId string `json:"pluginId"`
	Plugin   string `json:"plugin"`
	RepoName string `json:"repoName"`
	Url      string `json:"url"`
	Option   string `json:"option"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
