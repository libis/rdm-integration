package types

const NewDataset = "New Dataset"

type Metadata map[string]interface{}

type MetadataStruct struct {
	Title                   string
	AlternativeTitle        []string
	AlternativeURL          []string
	OtherId                 []OtherId
	Author                  []Author
	DsDescription           []string
	Keyword                 []string
	ContributorName         []string
	GrantNumber             []GrantNumber
	RelatedMaterialCitation []string
}

type OtherId struct {
	OtherIdAgency string
	OtherIdValue  string
}

type Author struct {
	AuthorName        string
	AuthorAffiliation string
	AuthorIdentifier  string
}

type GrantNumber struct {
	GrantNumberValue  string
	GrantNumberAgency string
}

type GetMetadataRequest struct {
	PluginId string `json:"pluginId"`
	Plugin   string `json:"plugin"`
	RepoName string `json:"repoName"`
	Url      string `json:"url"`
	Option   string `json:"option"`
	User     string `json:"user"`
	Token    string `json:"token"`
}
