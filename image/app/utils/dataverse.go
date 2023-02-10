// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	dv "integration/app/dataverse"
	"integration/app/tree"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

var dvContextDuration = 5 * time.Second
var deleteAndCleanupCtxDuration = 2 * time.Minute

func GetNodeMap(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	url := config.DataverseServer + "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
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
	res := dv.ListResponse{}
	err = json.Unmarshal(responseData, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		return nil, fmt.Errorf("listing files for %s failed: %+v", persistentId, res)
	}
	mapped := mapToNodes(res.Data)
	//check known hashes cache
	knownHashes := getKnownHashes(ctx, persistentId)
	for k, v := range mapped {
		if knownHashes[k].LocalHashValue == "" {
			continue
		}
		invalid := (knownHashes[k].LocalHashValue != v.Attributes.Metadata.DataFile.Checksum.Value)
		if invalid {
			invalidateKnownHashes(ctx, persistentId)
			break
		}
	}
	return mapped, nil
}

func mapToNodes(data []tree.Metadata) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, d := range data {
		dir := ""
		if d.DirectoryLabel != "" {
			dir = d.DirectoryLabel + "/"
		}
		id := dir + d.DataFile.Filename
		res[id] = tree.Node{
			Id:   id,
			Name: d.DataFile.Filename,
			Path: d.DirectoryLabel,
			Attributes: tree.Attributes{
				ParentId:  d.DirectoryLabel,
				Metadata:  d,
				IsFile:    true,
				LocalHash: d.DataFile.Checksum.Value,
			},
		}
	}
	return res
}

func requestBody(data []byte) (io.Reader, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormField("jsonData")
	part.Write(data)
	writer.Close()
	return body, writer.FormDataContentType()
}

func CheckPermission(ctx context.Context, token, user, persistentId string) error {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	if !checkPermissions {
		return nil
	}
	url := fmt.Sprintf("%s/api/v1/admin/permissions/:persistentId?persistentId=%s&unblock-key=%s", config.DataverseServer, persistentId, unblockKey)
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
	res := dv.Permissions{}
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

func getUser(ctx context.Context, token, user string) (dv.User, error) {
	url := fmt.Sprintf("%s/api/v1/users/:me", config.DataverseServer)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return dv.User{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return dv.User{}, err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return dv.User{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return dv.User{}, fmt.Errorf("getting user failed: %s", string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return dv.User{}, err
	}
	res := dv.User{}
	err = json.Unmarshal(b, &res)
	return res, err
}

func CreateNewDataset(ctx context.Context, collection, token, userName string) (string, error) {
	if collection == "" {
		collection = config.Options.RootDataverseId
	}
	if collection == "" {
		return "", fmt.Errorf("dataverse collection was not specified: unable to create a new dataset")
	}
	user, err := getUser(ctx, token, userName)
	if err != nil {
		return "", err
	}
	body := dv.CreateDatasetRequestBody(user)
	url := config.DataverseServer + "/api/v1/dataverses/" + collection + "/datasets?doNotValidate=true"
	url, addTokenToHeader, err := signUrl(ctx, url, token, userName)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return "", err
	}
	request.Header.Add("Content-Type", "application/json")
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 201 {
		b, _ := io.ReadAll(r.Body)
		return "", fmt.Errorf("creating dataset failed (%v): %s", r.StatusCode, string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	res := dv.CreateNewDatasetResponse{}
	err = json.Unmarshal(b, &res)
	return res.Data.PersistentId, err
}

func cleanup(ctx context.Context, token, user, persistentId string, writtenKeys []string) error {
	go cleanRedis(writtenKeys)
	if filesCleanup != "true" {
		return nil
	}
	shortContext, cancel := context.WithTimeout(ctx, deleteAndCleanupCtxDuration)
	defer cancel()
	url := config.DataverseServer + "/api/v1/datasets/:persistentId/cleanStorage?persistentId=" + persistentId
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
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	res := dv.CleanupResponse{}
	err = json.Unmarshal(responseData, &res)
	if err != nil {
		return err
	}
	if res.Status != "OK" {
		return fmt.Errorf("cleaning up files for %s failed: %+v", persistentId, res)
	}
	//logging.Logger.Println(res.Data.Message)
	return nil
}

func cleanRedis(writtenKeys []string) {
	time.Sleep(fileNamesInCacheDuration)
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	for _, k := range writtenKeys {
		GetRedis().Del(shortContext, k)
	}
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
	url := config.DataverseServer + fmt.Sprintf("/api/v1/datasets/:persistentId?persistentId=%s", persistentId)
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
	return fmt.Sprintf("%s/api/v1/admin/permissions/%v?&unblock-key=%s", config.DataverseServer, id, unblockKey), nil
}

func ListDvObjects(ctx context.Context, objectType, collection, token, user string) ([]dv.Item, error) {
	searchTerm := ""
	if collection != "" {
		searchTerm = "identifierOfDataverse=" + collection
	}
	res := []dv.Item{}
	hasNextPage := true
	roleIds := ""
	for _, v := range config.Options.MyDataRoleIds {
		roleIds = fmt.Sprintf("%v%v%v", roleIds, "&role_ids=", v)
	}
	for page := 1; hasNextPage; page++ {
		url := config.DataverseServer + "/api/v1/mydata/retrieve?" +
			"&selected_page=" + fmt.Sprint(page) +
			"&dvobject_types=" + objectType +
			"&published_states=Published&published_states=Unpublished&published_states=Draft&published_states=In%20Review" +
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
		retrieveResponse := dv.RetrieveResponse{}
		err = json.Unmarshal(responseData, &retrieveResponse)
		if err != nil {
			return nil, err
		}
		if !retrieveResponse.Success {
			return nil, fmt.Errorf("listing %v objects was not successful: %v", objectType, retrieveResponse.ErrorMessage)
		}
		res = append(res, retrieveResponse.Data.Items...)
		hasNextPage = retrieveResponse.Data.Pagination.HasNextPageNumber
	}
	return res, nil
}

func GetDatasetUrl(pid string, draft bool) string {
	draftVersion := "version=DRAFT&"
	if !draft {
		draftVersion = ""
	}
	url := config.DataverseServer
	if config.Options.DataverseExternalUrl != "" {
		url = config.Options.DataverseExternalUrl
	}
	return fmt.Sprintf("%v/dataset.xhtml?%vpersistentId=%v", url, draftVersion, pid)
}

func downloadFile(ctx context.Context, token, user string, id int64) (io.ReadCloser, error) {
	url := config.DataverseServer + fmt.Sprintf("/api/v1/access/datafile/%v", id)
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

func GetExternalDataverseURL() string {
	if config.Options.DataverseExternalUrl != "" {
		return config.Options.DataverseExternalUrl
	}
	return config.DataverseServer
}

func directAddReplaceFile(ctx context.Context, token, user, persistentId string, jsonData dv.JsonData) error {
	url := config.DataverseServer + "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	if jsonData.FileToReplaceId != 0 {
		url = config.DataverseServer + "/api/v1/files/" + fmt.Sprint(jsonData.FileToReplaceId) + "/replace"
	}
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	data, err := json.Marshal(jsonData)
	if err != nil {
		return err
	}
	body, formDataContentType := requestBody(data)
	request, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Add("Content-Type", formDataContentType)
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
		return fmt.Errorf("writing file in %s failed: %d - %s", persistentId, r.StatusCode, string(b))
	}
	b, _ := io.ReadAll(r.Body)
	res := dv.AddReplaceFileResponse{}
	err = json.Unmarshal(b, &res)
	if res.Status != "OK" {
		return fmt.Errorf("writting file failed: %+v", res)
	}
	return err
}

func apiAddReplaceFile(ctx context.Context, dbId int64, id, token, user, persistentId string, wg *sync.WaitGroup, async_err *ErrorHolder) (io.WriteCloser, error) {
	url := config.DataverseServer + "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	if dbId != 0 {
		url = config.DataverseServer + "/api/v1/files/" + fmt.Sprint(dbId) + "/replace"
	}
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return nil, err
	}

	filename, dir := splitId(id)
	jsonData := dv.JsonData{
		DirectoryLabel: dir,
		ForceReplace:   dbId != 0,
	}
	jsonDataBytes, _ := json.Marshal(jsonData)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	fw := newFileWriter(filename, jsonDataBytes, writer)

	request, _ := http.NewRequestWithContext(ctx, "POST", url, pr)
	request.Header.Add("Content-Type", writer.FormDataContentType())
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}

	wg.Add(1)
	go func(req *http.Request) {
		defer wg.Done()
		defer pr.Close()
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			if async_err != nil {
				async_err.Err = fmt.Errorf("writing file in %s failed: %s", persistentId, err)
			}
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			if async_err != nil {
				async_err.Err = fmt.Errorf("writing file in %s failed: %d - %s", persistentId, r.StatusCode, string(b))
			}
			return
		}
		b, _ := io.ReadAll(r.Body)
		res := dv.AddReplaceFileResponse{}
		json.Unmarshal(b, &res)
		if res.Status != "OK" {
			if async_err != nil {
				async_err.Err = fmt.Errorf("adding file failed: %+v", res)
			}
		}
	}(request)

	return writerCloser{fw, fw, pw}, nil
}

func splitId(id string) (string, string) {
	spl := strings.Split(id, "/")
	filename := spl[len(spl)-1]
	dir := ""
	if len(spl) > 1 {
		dir = strings.Join(spl[:len(spl)-1], "/")
	}
	return filename, dir
}

func signUrl(ctx context.Context, inUrl, token, user string) (string, bool, error) {
	if user == "" {
		return inUrl, true, nil
	}
	jsonString := fmt.Sprintf(`{"url":"%v","timeOut":500,"user":"%v"}`, inUrl, user)
	url := config.DataverseServer + "/api/v1/admin/requestSignedUrl?unblock-key=" + unblockKey
	request, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer([]byte(jsonString)))
	request.Header.Add("X-Dataverse-key", apiKey)
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
	res := dv.SignedUrlResponse{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return "", false, err
	}
	if res.Status != "OK" {
		return "", false, fmt.Errorf(res.Message)
	}
	return res.Data.SignedUrl, false, nil
}
