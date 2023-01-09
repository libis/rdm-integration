package types

type StreamParams struct {
	RepoName string `json:"repoName"`
	Url      string `json:"url"`
	Option   string `json:"option"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
