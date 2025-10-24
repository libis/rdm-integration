// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/libis/rdm-dataverse-go-api/api"
)

var dvContextDuration = 5 * time.Minute

func IsDirectUpload() bool {
	return directUpload == "true" && config.GetConfig().Options.DefaultDriver != ""
}

func GetRequest(path, method, user, token string, body io.Reader, header http.Header) *api.Request {
	client := api.NewClient(config.GetConfig().DataverseServer)
	client.User = user
	client.Token = token
	if urlSigning == "true" {
		client.AdminApiKey = config.ApiKey
		client.UnblockKey = config.UnblockKey
	}
	return client.NewRequest(path, method, body, header)
}

func GetNodeMap(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	path := "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
	res := api.ListResponse{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		return nil, fmt.Errorf("listing files for %s failed: %+v", persistentId, res)
	}
	mapped := mapToNodes(res.Data)
	//check known hashes cache
	core.CheckKnownHashes(ctx, persistentId, mapped)
	return mapped, nil
}

type datasetMetadataResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

func GetDatasetMetadata(ctx context.Context, persistentId, token, user string) ([]byte, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	queryPid := url.QueryEscape(persistentId)
	path := fmt.Sprintf("/api/v1/datasets/:persistentId?persistentId=%s&excludeFiles=true", queryPid)
	res := datasetMetadataResponse{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		return nil, fmt.Errorf("listing metadata for %s failed: status %s", persistentId, res.Status)
	}
	return res.Data, nil
}

func GetDataFileDDI(ctx context.Context, token, user string, fileID int64) ([]byte, error) {
	if fileID == 0 {
		return nil, fmt.Errorf("data file ID was not provided")
	}
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	path := fmt.Sprintf("/api/v1/access/datafile/%d/metadata/ddi", fileID)
	req := GetRequest(path, "GET", user, token, nil, nil)
	stream, err := api.DoStream(shortContext, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	body, err := io.ReadAll(stream)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty DDI response for file %d", fileID)
	}
	return body, nil
}

func mapToNodes(data []api.MetaData) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, d := range data {
		dir := ""
		if d.DirectoryLabel != "" {
			dir = d.DirectoryLabel + "/"
		}
		id := dir + d.DataFile.FileName
		hash := d.DataFile.Md5
		hashType := types.Md5
		if hash == "" {
			hash = d.DataFile.Checksum.Value
			hashType = d.DataFile.Checksum.Type
		}
		res[id] = tree.Node{
			Id:   id,
			Name: d.DataFile.FileName,
			Path: d.DirectoryLabel,
			Attributes: tree.Attributes{
				DestinationFile: tree.DestinationFile{
					Id:                d.DataFile.Id,
					FileSize:          d.DataFile.FileSize,
					Hash:              hash,
					HashType:          hashType,
					StorageIdentifier: d.DataFile.StorageIdentifier,
				},
				IsFile: true,
			},
		}
	}
	return res
}

func CheckPermission(ctx context.Context, token, user, persistentId string) error {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	if config.UnblockKey == "" {
		return nil
	}
	path := fmt.Sprintf("/api/v1/admin/permissions/:persistentId?persistentId=%s&unblock-key=%s", persistentId, config.UnblockKey)
	if slashInPermissions != "true" {
		var err error
		path, err = noSlashPermissionUrl(shortContext, persistentId, token, user)
		if err != nil {
			return err
		}
	}
	res := api.Permissions{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return err
	}
	if res.Status != "OK" {
		return fmt.Errorf("permission check status is %s for dataset %s", res.Status, persistentId)
	}
	for _, v := range res.Data.Permissions {
		if v == "EditDataset" {
			return nil
		}
	}
	return fmt.Errorf("user %v has no permission to edit dataset %v", res.Data.User, persistentId)
}

func noSlashPermissionUrl(ctx context.Context, persistentId, token, user string) (string, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	type Data struct {
		Id int `json:"id"`
	}
	type Res struct {
		Data `json:"data"`
	}
	path := "/api/v1/datasets/:persistentId?persistentId=" + persistentId
	res := Res{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return "", err
	}
	id := res.Id
	if id == 0 {
		return "", fmt.Errorf("dataset %v not found", persistentId)
	}
	return fmt.Sprintf("/api/v1/admin/permissions/%v?&unblock-key=%s", id, config.UnblockKey), nil
}

func GetDatasetUrl(pid string, draft bool) string {
	draftVersion := "version=DRAFT&"
	if !draft {
		draftVersion = ""
	}
	url := config.GetConfig().DataverseServer
	if config.GetConfig().Options.DataverseExternalUrl != "" {
		url = config.GetConfig().Options.DataverseExternalUrl
	}
	return fmt.Sprintf("%v/dataset.xhtml?%vpersistentId=%v", url, draftVersion, pid)
}

func DownloadFile(ctx context.Context, token, user string, id int64) (io.ReadCloser, error) {
	path := fmt.Sprintf("/api/v1/access/datafile/%v", id)
	req := GetRequest(path, "GET", user, token, nil, nil)
	return api.DoStream(ctx, req)
}

func DvObjects(ctx context.Context, objectType, collection, searchTerm, token, user string) ([]types.SelectItem, error) {
	dvObjects, err := listDvObjects(ctx, objectType, collection, searchTerm, token, user)
	if err != nil {
		return nil, err
	}
	res := []types.SelectItem{}
	added := map[string]bool{}
	for _, v := range dvObjects {
		id := v.GlobalId
		if id == "" {
			id = v.Identifier
		}
		label := v.Name + " (" + id + ")"
		if !added[label] {
			added[label] = true
			res = append(res, types.SelectItem{
				Label: label,
				Value: id,
			})
		}
	}
	return res, nil
}

func listDvObjects(ctx context.Context, objectType, collection, searchTermFirstPart, token, user string) ([]api.Item, error) {
	searchTerm := ""
	if searchTermFirstPart != "" {
		searchTerm = "text:\"" + searchTermFirstPart + "\""
		if collection != "" {
			searchTerm = " identifierOfDataverse:(+" + collection + ")"
		}
	} else if collection != "" {
		searchTerm = "identifierOfDataverse:(+" + collection + ")"
	}
	searchTerm = url.QueryEscape(searchTerm)
	res := []api.Item{}
	hasNextPage := true
	roleIds := ""
	for _, v := range config.GetConfig().Options.MyDataRoleIds {
		roleIds = fmt.Sprintf("%v%v%v", roleIds, "&role_ids=", v)
	}
	for page := 1; hasNextPage; page++ {
		path := "/api/v1/mydata/retrieve?" +
			"selected_page=" + fmt.Sprint(page) +
			"&dvobject_types=" + objectType +
			"&published_states=Published&published_states=Unpublished&published_states=Draft" +
			roleIds + "&mydata_search_term=" + searchTerm
		if urlSigning != "true" {
			path = path + "&key=" + token
		}

		retrieveResponse := api.RetrieveResponse{}
		req := GetRequest(path, "GET", user, token, nil, nil)
		err := api.Do(ctx, req, &retrieveResponse)
		if err != nil {
			return nil, err
		}

		if !retrieveResponse.Success {
			return nil, fmt.Errorf("listing %v objects was not successful: %v", objectType, retrieveResponse.ErrorMessage)
		}
		res = append(res, retrieveResponse.Data.Items...)
		hasNextPage = retrieveResponse.Data.Pagination.HasNextPageNumber && page < config.GetMaxDvObjectPages()
	}
	return res, nil
}

func GetUser(ctx context.Context, token, user string) (res api.User, err error) {
	path := "/api/v1/users/:me"
	req := GetRequest(path, "GET", user, token, nil, nil)
	err = api.Do(ctx, req, &res)
	return res, err
}

func GetUserEmail(ctx context.Context, token, user string) (string, error) {
	u, err := GetUser(ctx, token, user)
	if err != nil {
		return "", err
	}
	return u.Data.Email, nil
}

type DatasetVersionResponse struct {
	Status string                     `json:"status"`
	Data   DatasetVersionResponseData `json:"data"`
}

type DatasetVersionResponseData struct {
	DatasetPersistentId string `json:"datasetPersistentId"`
}

func GetDatasetVersion(ctx context.Context, datasetDbId, token, userName string) (string, error) {
	if datasetDbId == "" {
		return "", fmt.Errorf("dataset DB ID was not specified: unable to get latest version")
	}
	res := DatasetVersionResponse{}
	path := "/api/v1/datasets/" + datasetDbId + "/versions/:latest?excludeFiles=true"
	req := GetRequest(path, "GET", userName, token, nil, api.JsonContentHeader())
	err := api.Do(ctx, req, &res)
	return res.Data.DatasetPersistentId, err
}
