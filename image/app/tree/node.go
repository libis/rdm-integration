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
	URL             string          `json:"url"`
	RemoteHash      string          `json:"remoteHash"`
	RemoteHashType  string          `json:"remoteHashType"`
	RemoteFilesize  int64           `json:"remoteFilesize"`
	IsFile          bool            `json:"isFile"`
	DestinationFile DestinationFile `json:"destinatinFile"`
}

type DestinationFile struct {
	Id                int64  `json:"id"`
	Filesize          int64  `json:"filesize"`
	Hash              string `json:"hash"`
	HashType          string `json:"hashType"`
	StorageIdentifier string `json:"storageIdentifier"`
}
