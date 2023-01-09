package types

type CompareRequest struct {
	RepoType     string `json:"repoType"`
	RepoName     string `json:"repoName"`
	Url          string `json:"url"`
	Option       string `json:"option"`
	User         string `json:"user"`
	Token        string `json:"token"`
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
}
