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
	"mime/multipart"
	"net/http"
	"strings"
	"sync"

	"github.com/libis/rdm-dataverse-go-api/api"
)

func CreateNewDataset(ctx context.Context, collection, token, userName string, metadata types.Metadata) (string, error) {
	if collection == "" {
		collection = config.GetConfig().Options.RootDataverseId
	}
	if collection == "" {
		return "", fmt.Errorf("dataverse collection was not specified: unable to create a new dataset")
	}
	user, err := GetUser(ctx, token, userName)
	if err != nil {
		return "", err
	}
	var body io.Reader = nil
	if len(metadata) == 0 {
		body = api.CreateDatasetRequestBody(user)
	} else {
		metadata["license"], err = getDefaultLicense(ctx, token, userName)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(metadata)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(data)
	}
	res := api.CreateNewDatasetResponse{}
	path := "/api/v1/dataverses/" + collection + "/datasets?doNotValidate=true"
	req := GetRequest(path, "POST", userName, token, body, api.JsonContentHeader())
	err = api.Do(ctx, req, &res)
	return res.Data.PersistentId, err
}

func getDefaultLicense(ctx context.Context, user, token string) (map[string]interface{}, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	path := "/api/v1/licenses"
	res := map[string]interface{}{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return nil, err
	}
	data, ok := res["data"].([]map[string]interface{})
	if !ok || res["status"] != "OK" {
		return nil, fmt.Errorf("listing licenses failed: %+v", res)
	}

	for _, license := range data {
		if d, ok := license["isDefault"].(bool); ok && d {
			name, _ := license["name"].(string)
			uri, _ := license["uri"].(string)
			return map[string]interface{}{
				"name": name,
				"uri":  uri,
			}, nil
		}
	}

	return map[string]interface{}{}, nil
}

func SaveAfterDirectUpload(ctx context.Context, replace bool, token, user, persistentId string, storageIdentifiers []string, nodes []tree.Node) error {
	jsonData := []api.JsonData{}
	for i, v := range nodes {
		jsonData = append(jsonData, api.JsonData{
			FileToReplaceId:   v.Attributes.DestinationFile.Id,
			ForceReplace:      v.Attributes.DestinationFile.Id != 0,
			StorageIdentifier: storageIdentifiers[i],
			FileName:          v.Name,
			DirectoryLabel:    v.Path,
			MimeType:          "application/octet-stream", // default that will be replaced by Dataverse while adding/replacing the file
			TabIngest:         false,
			Checksum: &api.Checksum{
				Type:  v.Attributes.DestinationFile.HashType,
				Value: v.Attributes.DestinationFile.Hash,
			},
		})
	}

	path := "/api/v1/datasets/:persistentId/addFiles?persistentId=" + persistentId
	if replace {
		path = "/api/v1/datasets/:persistentId/replaceFiles?persistentId=" + persistentId
	}
	data, err := json.Marshal(jsonData)
	if err != nil {
		return err
	}
	body, formDataContentType := requestBody(data)
	res := api.AddReplaceFileResponse{}
	reqHeader := http.Header{}
	reqHeader.Add("Content-Type", formDataContentType)
	req := GetRequest(path, "POST", user, token, body, reqHeader)
	err = api.Do(ctx, req, &res)
	if err != nil {
		return err
	}

	if res.Status != "OK" {
		return fmt.Errorf("writing file failed: %+v", res)
	}
	return nil
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

	path := "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	if dbId != 0 {
		path = config.GetConfig().DataverseServer + "/api/v1/files/" + fmt.Sprint(dbId) + "/replace"
	}

	filename, dir := splitId(id)
	jsonData := api.JsonData{
		DirectoryLabel: dir,
		ForceReplace:   dbId != 0,
	}
	jsonDataBytes, _ := json.Marshal(jsonData)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	fw := core.NewFileWriter(filename, jsonDataBytes, writer)

	requestHeader := http.Header{}
	requestHeader.Add("Content-Type", writer.FormDataContentType())

	request := GetRequest(path, "POST", user, token, pr, requestHeader)

	wg.Add(1)
	go func(req *api.Request) {
		defer wg.Done()
		defer pr.Close()
		res := api.AddReplaceFileResponse{}
		err := api.Do(ctx, request, &res)
		if err != nil {
			if async_err != nil {
				async_err.Err = fmt.Errorf("writing file in %s failed: %s", persistentId, err)
			}
			return
		}
		if res.Status != "OK" {
			if async_err != nil {
				async_err.Err = fmt.Errorf("adding file failed: %+v", res)
			}
		}
	}(request)

	return core.NewWriterCloser(fw, fw, pw), nil
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
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/cleanStorage?persistentId=" + persistentId
	res := api.CleanupResponse{}
	req := GetRequest(path, "GET", user, token, nil, nil)
	err := api.Do(ctx, req, &res)
	if err != nil {
		return err
	}
	if res.Status != "OK" {
		return fmt.Errorf("cleaning up files for %s failed: %+v", persistentId, res)
	}
	return nil
}

func DeleteFile(ctx context.Context, token, user string, id int64) error {
	if nativeApiDelete != "true" {
		return swordDelete(ctx, token, user, id)
	}
	path := "/api/v1/files/" + fmt.Sprint(id)
	res := api.DvResponse{}
	req := GetRequest(path, "DELETE", user, token, nil, nil)
	err := api.Do(ctx, req, &res)
	if err != nil {
		return err
	}
	if res.Status != "OK" {
		return fmt.Errorf("deleting file %d failed: %s", id, res.Message)
	}
	return nil
}
