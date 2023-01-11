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

type DvResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type CleanupResponse struct {
	Status string      `json:"status"`
	Data   CleanupData `json:"data"`
}

type CleanupData struct {
	Message string `json:"message"`
}

type ListResponse struct {
	DvResponse
	Data []tree.Metadata `json:"data"`
}

type Permissions struct {
	DvResponse
	Data PermissionsData `json:"data"`
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

type AddFilesResponse struct {
	Status string               `json:"status"`
	Data   AddFilesResponseData `json:"data"`
}

type AddFilesResponseData struct {
	Files  []AddFilesFile
	Result AddFilesResponseResult
}

type AddFilesFile struct {
	ErrorMessage string   `json:"errorMessage"`
	FileDetails  JsonData `json:"fileDetails"`
}

type AddFilesResponseResult struct {
	Total int `json:"Total number of files"`
	Added int `json:"Number of files successfully added"`
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

func CreateDatasetRequestBody(user User) io.Reader {
	data := []byte(fmt.Sprintf(createDatasetRequestFormat, user.Data.LastName, user.Data.FirstName))
	return bytes.NewReader(data)
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

type RetrieveResponse struct {
	Success bool                 `json:"success"`
	Data    RetrieveResponseData `json:"data"`
}

type RetrieveResponseData struct {
	Pagination      Pagination      `json:"pagination"`
	Items           []Item          `json:"items"`
	TotoalCount     int             `json:"total_count"`
	Start           int             `json:"start"`
	SearchTerm      string          `json:"search_term"`
	DvObjectCounts  DvObjectCounts  `json:"dvobject_counts"`
	PubstatusCounts PubstatusCounts `json:"pubstatus_counts"`
	SelectedFilters SelectedFilters `json:"selected_filters"`
}

type DvObjectCounts struct {
	DataversesCount int `json:"dataverses_count"`
	FilesCount      int `json:"files_count"`
	DatasetsCount   int `json:"datasets_count"`
}

type PubstatusCounts struct {
	UnpublishedCount   int `json:"unpublished_count"`
	DraftCount         int `json:"draft_count"`
	PublishedCount     int `json:"published_count"`
	InReviewCount      int `json:"in_review_count"`
	DeaccessionedCount int `json:"deaccessioned_count"`
}

type SelectedFilters struct {
	PublicationStatuses []string `json:"publication_statuses"`
	RoleNames           []string `json:"role_names"`
}

type Pagination struct {
	IsNecessary           bool   `json:"isNecessary"`
	NumResults            int    `json:"numResults"`
	NumResultsString      string `json:"numResultsString"`
	DocsPerPage           int    `json:"docsPerPage"`
	SelectedPageNumber    int    `json:"selectedPageNumber"`
	PageCount             int    `json:"pageCount"`
	HasPreviousPageNumber bool   `json:"hasPreviousPageNumber"`
	PreviousPageNumber    int    `json:"previousPageNumber"`
	HasNextPageNumber     bool   `json:"hasNextPageNumber"`
	NextPageNumber        int    `json:"nextPageNumber"`
	StartCardNumber       int    `json:"startCardNumber"`
	EndCardNumber         int    `json:"endCardNumber"`
	StartCardNumberString string `json:"startCardNumberString"`
	EndCardNumberString   string `json:"endCardNumberString"`
	RemainingCards        int    `json:"remainingCards"`
	NumberNextResults     int    `json:"numberNextResults"`
	PageNumberList        []int  `json:"pageNumberList"`
}
type Item struct {
	Name                  string    `json:"name"`
	Type                  string    `json:"type"`
	Url                   string    `json:"url"`
	GlobalId              string    `json:"global_id"`
	Identifier            string    `json:"identifier"`
	Description           string    `json:"description"`
	Publisher             string    `json:"publisher"`
	CitationHtml          string    `json:"citationHtml"`
	IdentifierOfDataverse string    `json:"identifier_of_dataverse"`
	NameOfDataverse       string    `json:"name_of_dataverse"`
	Citation              string    `json:"citation"`
	Score                 float64   `json:"score"`
	EntityId              int       `json:"entity_id"`
	StorageIdentifier     string    `json:"storageIdentifier"`
	Keywords              []string  `json:"keywords"`
	FileCount             int       `json:"fileCount"`
	VersionId             int       `json:"versionId"`
	VersionState          string    `json:"versionState"`
	CreatedAt             string    `json:"createdAt"`
	UpdatedAt             string    `json:"updatedAt"`
	Contacts              []Contact `json:"contacts"`
	ApiUrl                string    `json:"api_url"`
	Authors               []string  `json:"authors"`
	PublicationStatuses   []string  `json:"publication_statuses"`
	IsDraftState          bool      `json:"is_draft_state"`
	IsInReviewState       bool      `json:"is_in_review_state"`
	IsUnpublishedState    bool      `json:"is_unpublished_state"`
	IsPublished           bool      `json:"is_published"`
	IsDeaccesioned        bool      `json:"is_deaccesioned"`
	DateToDisplayOnCard   string    `json:"date_to_display_on_card"`
	ParentId              string    `json:"parentId"`
	ParentName            string    `json:"parentName"`
	ParentAlias           string    `json:"parent_alias"`
	UserRoles             []string  `json:"user_roles"`
	//`json:"matches"`//: [],
	//`json:"subjects"`//: [],
}

type Contact struct {
	Name        string `json:"name"`
	Affiliation string `json:"affiliation"`
}
