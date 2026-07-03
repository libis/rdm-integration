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
	URL            string `json:"url"`
	RemoteHash     string `json:"remoteHash"`
	RemoteHashType string `json:"remoteHashType"`
	RemoteFileSize int64  `json:"remoteFileSize"`
	IsFile         bool   `json:"isFile"`
	// MimeType, when set by a source plugin, is sent to the destination on
	// upload instead of relying on destination-side type detection. Used for
	// generated metadata files (e.g. JSON-LD sidecars) whose extension alone
	// does not yield the mime type that previewers are registered for.
	MimeType        string          `json:"mimeType,omitempty"`
	DestinationFile DestinationFile `json:"destinationFile"`
}

type DestinationFile struct {
	Id                int64  `json:"id"`
	FileSize          int64  `json:"fileSize"`
	Hash              string `json:"hash"`
	HashType          string `json:"hashType"`
	StorageIdentifier string `json:"storageIdentifier"`
}
