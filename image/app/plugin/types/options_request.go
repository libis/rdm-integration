// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

type OptionsRequest struct {
	RepoType string `json:"repoType"`
	RepoName string `json:"repoName"`
	Url      string `json:"url"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
