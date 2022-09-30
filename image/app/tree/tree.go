package tree

type Node struct {
	Id         string     `json:"id"`
	Html       string     `json:"html"`
	Attributes Attributes `json:"attributes"`
	Children   []*Node    `json:"children"`
	Checked    bool       `json:"checked"`
}

type Attributes struct {
	URL            string   `json:"url"`
	ParentId       string   `json:"parentId"`
	LocalHash      string   `json:"localHash"`
	RemoteHash     string   `json:"remoteHash"`
	RemoteHashType string   `json:"remoteHashType"`
	IsFile         bool     `json:"isFile"`
	SomeMatch      *bool    `json:"someMatch,omitempty"`
	AllMatch       *bool    `json:"allMatch,omitempty"`
	AllLocal       *bool    `json:"allMatch,omitempty"`
	Metadata       Metadata `json:"metadata"`
}

type Metadata struct {
	Description      string   `json:"description"`
	Label            string   `json:"label"`
	Restricted       bool     `json:"restricted"`
	DirectoryLabel   string   `json:"directoryLabel"`
	Version          int      `json:"version"`
	DatasetVersionId int      `json:"datasetVersionId"`
	Categories       []string `json:"categories"`
	DataFile         DataFile `json:"dataFile"`
}

type DataFile struct {
	Id                int      `json:"id"`
	PersistentId      string   `json:"persistentId"`
	PidURL            string   `json:"pidURL"`
	Filename          string   `json:"filename"`
	ContentType       string   `json:"contentType"`
	Filesize          int      `json:"filesize"`
	Description       string   `json:"description"`
	StorageIdentifier string   `json:"storageIdentifier"`
	RootDataFileId    int      `json:"rootDataFileId"`
	Md5               string   `json:"md5"`
	Checksum          Checksum `json:"checksum"`
	CreationDate      string   `json:"creationDate"`
}

type Checksum struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
