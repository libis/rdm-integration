package types

type OptionsRequest struct {
	RepoType string `json:"repoType"`
	RepoName string `json:"repoName"`
	Url      string `json:"url"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
