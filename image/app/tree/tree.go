// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package tree

const (
	Equal   = 0
	New     = 1
	Updated = 2
	Deleted = 3
	Unknown = 4
)

const (
	Ignore = 0
	Copy   = 1
	Update = 2
	Delete = 3
)

type Node struct {
	Id         string     `json:"id"`
	Attributes Attributes `json:"attributes"`
	Path       string     `json:"path"`
	Name       string     `json:"name"`
	Status     int        `json:"status"`
	Action     int        `json:"action"`
}

type Attributes struct {
	URL            string   `json:"url"`
	ParentId       string   `json:"parentId"`
	LocalHash      string   `json:"localHash"`
	RemoteHash     string   `json:"remoteHash"`
	RemoteHashType string   `json:"remoteHashType"`
	IsFile         bool     `json:"isFile"`
	Metadata       Metadata `json:"metadata"`
}

type Metadata struct {
	Description      string   `json:"description"`
	Label            string   `json:"label"`
	Restricted       bool     `json:"restricted"`
	DirectoryLabel   string   `json:"directoryLabel"`
	Version          int64    `json:"version"`
	DatasetVersionId int64    `json:"datasetVersionId"`
	Categories       []string `json:"categories"`
	DataFile         DataFile `json:"dataFile"`
}

type DataFile struct {
	Id                int64    `json:"id"`
	PersistentId      string   `json:"persistentId"`
	PidURL            string   `json:"pidURL"`
	Filename          string   `json:"filename"`
	ContentType       string   `json:"contentType"`
	Filesize          int64    `json:"filesize"`
	Description       string   `json:"description"`
	StorageIdentifier string   `json:"storageIdentifier"`
	RootDataFileId    int64    `json:"rootDataFileId"`
	Md5               string   `json:"md5"`
	Checksum          Checksum `json:"checksum"`
	CreationDate      string   `json:"creationDate"`
}

type Checksum struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
