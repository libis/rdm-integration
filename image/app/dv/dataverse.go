package dv

import "integration/app/tree"

type JsonData struct {
	Description       *string  `json:"description,omitempty"`
	DirectoryLabel    *string  `json:"directoryLabel,omitempty"`
	Categories        []string `json:"categories,omitempty"`
	Restrict          *bool    `json:"restrict,omitempty"`
	StorageIdentifier string   `json:"storageIdentifier"`
	FileName          string   `json:"fileName"`
	MimeType          string   `json:"mimeType"`
	Checksum          Checksum `json:"checksum"`
}

type Checksum struct {
	Type  string `json:"@type"`
	Value string `json:"@value"`
}

type ListResponse struct {
	Status  string          `json:"status"`
	Data    []tree.Metadata `json:"data"`
	Message string          `json:"message"`
}

type Permissions struct {
	Status  string          `json:"status"`
	Data    PermissionsData `json:"data"`
	Message string          `json:"message"`
}

type PermissionsData struct {
	User  string          `json:"user"`
	Permissions []string          `json:"permissions"`
}
