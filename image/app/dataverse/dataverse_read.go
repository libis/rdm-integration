// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"bytes"
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
)

var dvContextDuration = 5 * time.Minute

func IsDirectUpload() bool {
	return directUpload == "true" && config.GetConfig().Options.DefaultDriver != ""
}

func GetNodeMap(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	url := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(shortContext, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	res := ListResponse{}
	err = json.Unmarshal(responseData, &res)
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

func mapToNodes(data []MetaData) map[string]tree.Node {
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
					Filesize:          d.DataFile.FileSize,
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
	url := fmt.Sprintf("%s/api/v1/admin/permissions/:persistentId?persistentId=%s&unblock-key=%s", config.GetConfig().DataverseServer, persistentId, config.UnblockKey)
	if slashInPermissions != "true" {
		var err error
		url, err = noSlashPermissionUrl(shortContext, persistentId, token, user)
		if err != nil {
			return err
		}
	}
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(shortContext, "GET", url, nil)
	if err != nil {
		return err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("getting permissions for dataset %s failed: %s", persistentId, string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	res := Permissions{}
	err = json.Unmarshal(b, &res)
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
	url := config.GetConfig().DataverseServer + fmt.Sprintf("/api/v1/datasets/:persistentId?persistentId=%s", persistentId)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return "", err
	}
	res := Res{}
	request, _ := http.NewRequestWithContext(shortContext, "GET", url, nil)
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	json.Unmarshal(responseData, &res)
	id := res.Id
	if id == 0 {
		return "", fmt.Errorf("dataset %v not found", persistentId)
	}
	return fmt.Sprintf("%s/api/v1/admin/permissions/%v?&unblock-key=%s", config.GetConfig().DataverseServer, id, config.UnblockKey), nil
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
	url := config.GetConfig().DataverseServer + fmt.Sprintf("/api/v1/access/datafile/%v", id)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func DeleteFile(ctx context.Context, token, user string, id int64) error {
	if nativeApiDelete != "true" {
		return swordDelete(ctx, token, user, id)
	}

	url := fmt.Sprintf("%s/api/v1/files/%d", config.GetConfig().DataverseServer, id)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	res := DvResponse{}
	json.Unmarshal(b, &res)
	if r.StatusCode != 200 || res.Status != "OK" {
		return fmt.Errorf("deleting file %d failed: %d - %s", id, r.StatusCode, res.Message)
	}
	return nil
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

func listDvObjects(ctx context.Context, objectType, collection, searchTermFirstPart, token, user string) ([]Item, error) {
	searchTerm := ""
	if searchTermFirstPart != "" {
		searchTerm = "text:(" + searchTermFirstPart + ")"
		if collection != "" {
			searchTerm = " identifierOfDataverse:(+" + collection + ")"
		}
	} else if collection != "" {
		searchTerm = "identifierOfDataverse:(+" + collection + ")"
	}
	searchTerm = url.QueryEscape(searchTerm)
	res := []Item{}
	hasNextPage := true
	roleIds := ""
	for _, v := range config.GetConfig().Options.MyDataRoleIds {
		roleIds = fmt.Sprintf("%v%v%v", roleIds, "&role_ids=", v)
	}
	for page := 1; hasNextPage; page++ {
		url := config.GetConfig().DataverseServer + "/api/v1/mydata/retrieve?" +
			"selected_page=" + fmt.Sprint(page) +
			"&dvobject_types=" + objectType +
			"&published_states=Published&published_states=Unpublished&published_states=Draft" +
			roleIds + "&mydata_search_term=" + searchTerm
		url, addTokenToUrl, err := signUrl(ctx, url, token, user)
		if err != nil {
			return nil, err
		}
		if addTokenToUrl {
			url = url + "&key=" + token
		}
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		//request.Header.Add("X-Dataverse-key", token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		responseData, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != 200 && response.StatusCode != 201 {
			return nil, fmt.Errorf("listing %v objects failed: %v", objectType, string(responseData))
		}
		retrieveResponse := RetrieveResponse{}
		err = json.Unmarshal(responseData, &retrieveResponse)
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

func signUrl(ctx context.Context, inUrl, token, user string) (string, bool, error) {
	if urlSigning != "true" || user == "" {
		return inUrl, true, nil
	}
	jsonString := fmt.Sprintf(`{"url":"%v","timeOut":500,"user":"%v"}`, inUrl, user)
	url := config.GetConfig().DataverseServer + "/api/v1/admin/requestSignedUrl?unblock-key=" + config.UnblockKey
	request, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer([]byte(jsonString)))
	request.Header.Add("X-Dataverse-key", config.ApiKey)
	request.Header.Add("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}
	res := SignedUrlResponse{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return "", false, err
	}
	if res.Status != "OK" {
		return "", false, fmt.Errorf(res.Message)
	}
	return res.Data.SignedUrl, false, nil
}
