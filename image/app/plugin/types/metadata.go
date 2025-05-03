package types

import "integration/app/tree"

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
	PluginId      string          `json:"pluginId"`
	Plugin        string          `json:"plugin"`
	RepoName      string          `json:"repoName"`
	Url           string          `json:"url"`
	Option        string          `json:"option"`
	User          string          `json:"user"`
	Token         string          `json:"token"`
	CompareResult CompareResponse `json:"compareResult"`
}

type CompareResponse struct {
	Id          string      `json:"id"`
	Status      int         `json:"status"`
	Data        []tree.Node `json:"data"`
	Url         string      `json:"url"`
	MaxFileSize int64       `json:"maxFileSize,omitempty"`
	Rejected    []string    `json:"rejected,omitempty"`
}
