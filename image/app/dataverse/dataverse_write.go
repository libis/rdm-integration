// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/tree"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
)

func CreateNewDataset(ctx context.Context, collection, token, userName string) (string, error) {
	if collection == "" {
		collection = config.GetConfig().Options.RootDataverseId
	}
	if collection == "" {
		return "", fmt.Errorf("dataverse collection was not specified: unable to create a new dataset")
	}
	user, err := getUser(ctx, token, userName)
	if err != nil {
		return "", err
	}
	body := CreateDatasetRequestBody(user)
	url := config.GetConfig().DataverseServer + "/api/v1/dataverses/" + collection + "/datasets?doNotValidate=true"
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
	res := CreateNewDatasetResponse{}
	err = json.Unmarshal(b, &res)
	return res.Data.PersistentId, err
}

func getUser(ctx context.Context, token, user string) (User, error) {
	url := fmt.Sprintf("%s/api/v1/users/:me", config.GetConfig().DataverseServer)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return User{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return User{}, err
	}
	if addTokenToHeader {
		request.Header.Add("X-Dataverse-key", token)
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return User{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return User{}, fmt.Errorf("getting user failed: %s", string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return User{}, err
	}
	res := User{}
	err = json.Unmarshal(b, &res)
	return res, err
}

func GetUserEmail(ctx context.Context, token, user string) (string, error) {
	u, err := getUser(ctx, token, user)
	if err != nil {
		return "", err
	}
	return u.Data.Email, nil
}

func SaveAfterDirectUpload(ctx context.Context, token, user, persistentId, storageIdentifier string, v tree.Node) error {
	jsonData := JsonData{
		FileToReplaceId:   v.Attributes.DestinationFile.Id,
		ForceReplace:      v.Attributes.DestinationFile.Id != 0,
		StorageIdentifier: storageIdentifier,
		FileName:          v.Name,
		DirectoryLabel:    v.Path,
		MimeType:          "application/octet-stream", // default that will be replaced by Dataverse while adding/replacing the file
		TabIngest:         false,
		Checksum: &Checksum{
			Type:  v.Attributes.DestinationFile.HashType,
			Value: v.Attributes.DestinationFile.Hash,
		},
	}
	url := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	if jsonData.FileToReplaceId != 0 {
		url = config.GetConfig().DataverseServer + "/api/v1/files/" + fmt.Sprint(jsonData.FileToReplaceId) + "/replace"
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
	res := AddReplaceFileResponse{}
	err = json.Unmarshal(b, &res)
	if res.Status != "OK" {
		return fmt.Errorf("writting file failed: %+v", res)
	}
	return err
}

func requestBody(data []byte) (io.Reader, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormField("jsonData")
	part.Write(data)
	writer.Close()
	return body, writer.FormDataContentType()
}

func ApiAddReplaceFile(ctx context.Context, dbId int64, id, token, user, persistentId string, wg *sync.WaitGroup, async_err *core.ErrorHolder) (io.WriteCloser, error) {
	if strings.HasSuffix(id, ".zip") {
		// workaround: upload via SWORD api
		if dbId != 0 {
			err := DeleteFile(ctx, token, user, dbId)
			if err != nil {
				return nil, err
			}
		}
		return uploadViaSword(ctx, dbId, id, token, user, persistentId, wg, async_err)
	}

	url := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	if dbId != 0 {
		url = config.GetConfig().DataverseServer + "/api/v1/files/" + fmt.Sprint(dbId) + "/replace"
	}
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return nil, err
	}

	filename, dir := splitId(id)
	jsonData := JsonData{
		DirectoryLabel: dir,
		ForceReplace:   dbId != 0,
	}
	jsonDataBytes, _ := json.Marshal(jsonData)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	fw := core.NewFileWriter(filename, jsonDataBytes, writer)

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
		res := AddReplaceFileResponse{}
		json.Unmarshal(b, &res)
		if res.Status != "OK" {
			if async_err != nil {
				async_err.Err = fmt.Errorf("adding file failed: %+v", res)
			}
		}
	}(request)

	return core.NewWritterCloser(fw, fw, pw), nil
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

func CleanupLeftOverFiles(ctx context.Context, persistentId, token, user string) error {
	if filesCleanup != "true" {
		return nil
	}
	url := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/cleanStorage?persistentId=" + persistentId
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	res := CleanupResponse{}
	err = json.Unmarshal(responseData, &res)
	if err != nil {
		return err
	}
	if res.Status != "OK" {
		return fmt.Errorf("cleaning up files for %s failed: %+v", persistentId, res)
	}
	return nil
}
