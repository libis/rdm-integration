package dv

import (
	"bytes"
	"fmt"
	"integration/app/tree"
	"io"
	"mime/multipart"
	"time"
)

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
	User        string   `json:"user"`
	Permissions []string `json:"permissions"`
}

type User struct {
	Status string   `json:"status"`
	Data   UserData `json:"data"`
}

type UserData struct {
	Id                       int       `json:"id"`
	Identifier               string    `json:"identifier"`
	DisplayName              string    `json:"displayName"`
	FirstName                string    `json:"firstName"`
	LastName                 string    `json:"lastName"`
	Email                    string    `json:"email"`
	Superuser                bool      `json:"superuser"`
	Deactivated              bool      `json:"deactivated"`
	Affiliation              string    `json:"affiliation"`
	Position                 string    `json:"position"`
	PersistentUserId         string    `json:"persistentUserId"`
	CreatedTime              time.Time `json:"createdTime"`
	LastLoginTime            time.Time `json:"lastLoginTime"`
	LastApiUseTime           time.Time `json:"lastApiUseTime"`
	AuthenticationProviderId string    `json:"authenticationProviderId"`
}

type CreateNewDatasetResponse struct {
	Status string                       `json:"status"`
	Data   CreateNewDatasetResponseData `json:"data"`
}

type CreateNewDatasetResponseData struct {
	Id           int    `json:"id"`
	PersistentId string `json:"persistentId"`
}

var createDatasetRequestFormat = `
{ 
	"datasetVersion": {
		"metadataBlocks": {
			"citation": {
				"fields": [{
					"value": [{
						"authorName": {
							"value": "%v, %v",
							"typeClass": "primitive",
							"multiple": false,
							"typeName": "authorName"
						}
					}],
					"typeClass": "compound",
					"multiple": true,
					"typeName": "author"
				}],
				"displayName": "Citation Metadata"
			}
		}
	}
}
`

func CreateDatasetRequestBody(user User) (io.Reader, string, error) {
	data := []byte(fmt.Sprintf(createDatasetRequestFormat, user.Data.LastName, user.Data.FirstName))
	return RequestBody(data)
}

func RequestBody(data []byte) (io.Reader, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormField("jsonData")
	if err != nil {
		return nil, "", err
	}
	part.Write(data)
	writer.Close()
	return body, writer.FormDataContentType(), nil
}
