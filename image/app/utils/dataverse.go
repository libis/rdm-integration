// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	dv "integration/app/dataverse"
	"integration/app/logging"
	"integration/app/tree"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
)

func GetNodeMap(persistentId, token string) (map[string]tree.Node, error) {
	url := config.DataverseServer + "/api/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("X-Dataverse-key", token)
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
	knownHashes := getKnownHashes(persistentId)
	for k, v := range mapped {
		if knownHashes[k].LocalHashValue == "" {
			continue
		}
		invalid := (knownHashes[k].LocalHashValue != v.Attributes.Metadata.DataFile.Checksum.Value)
		if invalid {
			invalidateKnownHashes(persistentId)
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

func deleteFromDV(dataverseKey string, id int) error {
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", config.DataverseServer, id)
	request, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		logging.Logger.Println(err)
		return err
	}
	request.SetBasicAuth(dataverseKey, "")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 202 && r.StatusCode != 204 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("deleting file %d failed: %d - %s", id, r.StatusCode, string(b))
	}
	return nil
}

func writeToDV(dataverseKey, persistentId string, jsonData dv.JsonData) error {
	url := config.DataverseServer + "/api/datasets/:persistentId/addFiles?persistentId=" + persistentId
	data, err := json.Marshal([]dv.JsonData{jsonData})
	if err != nil {
		return err
	}
	body, formDataContentType := requestBody(data)
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Add("Content-Type", formDataContentType)
	request.Header.Add("X-Dataverse-key", dataverseKey)
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
	res := dv.AddFilesResponse{}
	err = json.Unmarshal(b, &res)
	if res.Data.Result.Added != 1 {
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

func CheckPermission(dataverseKey, persistentId string) error {
	if !checkPermissions {
		return nil
	}
	url := fmt.Sprintf("%s/api/admin/permissions/:persistentId?persistentId=%s&unblock-key=%s", config.DataverseServer, persistentId, unblockKey)
	if slashInPermissions != "true" {
		var err error
		url, err = noSlashPermissionUrl(persistentId, dataverseKey)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	request.Header.Add("X-Dataverse-key", dataverseKey)
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

func getUser(dataverseKey string) (dv.User, error) {
	url := fmt.Sprintf("%s/api/users/:me", config.DataverseServer)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return dv.User{}, err
	}
	request.Header.Add("X-Dataverse-key", dataverseKey)
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

func CreateNewDataset(collection, dataverseKey string) (string, error) {
	if collection == "" {
		collection = config.Options.RootDataverseId
	}
	if collection == "" {
		return "", fmt.Errorf("dataverse collection was not specified: unable to create a new dataset")
	}
	user, err := getUser(dataverseKey)
	if err != nil {
		return "", err
	}
	body := dv.CreateDatasetRequestBody(user)
	url := config.DataverseServer + "/api/dataverses/" + collection + "/datasets?doNotValidate=true"
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return "", err
	}
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("X-Dataverse-key", dataverseKey)
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

func cleanup(token, persistentId string) error {
	if filesCleanup != "true" {
		return nil
	}
	url := config.DataverseServer + "/api/datasets/:persistentId/cleanStorage?persistentId=" + persistentId
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	request.Header.Add("X-Dataverse-key", token)
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

func noSlashPermissionUrl(persistentId, dataverseKey string) (string, error) {
	type Data struct {
		Id int `json:"id"`
	}
	type Res struct {
		Data `json:"data"`
	}
	res := Res{}
	request, _ := http.NewRequest("GET", config.DataverseServer+fmt.Sprintf("/api/datasets/:persistentId?persistentId=%s", persistentId), nil)
	request.Header.Add("X-Dataverse-key", dataverseKey)
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
	return fmt.Sprintf("%s/api/admin/permissions/%v?&unblock-key=%s", config.DataverseServer, id, unblockKey), nil
}

func ListDvObjects(objectType, collection, token string) ([]dv.Item, error) {
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
		url := config.DataverseServer + "/api/v1/mydata/retrieve?key=" + token +
			"&selected_page=" + fmt.Sprint(page) +
			"&dvobject_types=" + objectType +
			"&published_states=Published&published_states=Unpublished&published_states=Draft&published_states=In%20Review" +
			roleIds + "&mydata_search_term=" + searchTerm
		request, err := http.NewRequest("GET", url, nil)
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

func downloadFile(dataverseKey string, id int) (io.ReadCloser, error) {
	request, err := http.NewRequest("GET", config.DataverseServer+fmt.Sprintf("/api/access/datafile/%v", id), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("X-Dataverse-key", dataverseKey)
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

type ErrorHolder struct {
	Err error
}

func swordAddFile(dataverseKey, persistentId string, pr io.Reader, wg *sync.WaitGroup, async_err *ErrorHolder) {
	defer wg.Done()
	url := config.DataverseServer + "/dvn/api/data-deposit/v1.1/swordv2/edit-media/study/" + persistentId
	request, _ := http.NewRequest("POST", url, pr)
	request.Header.Add("Content-Type", "application/zip")
	request.Header.Add("Content-Disposition", "filename=example.zip")
	request.Header.Add("Packaging", "http://purl.org/net/sword/package/SimpleZip")
	request.SetBasicAuth(dataverseKey, "")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		if async_err != nil {
			async_err.Err = err
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && async_err != nil {
		b, _ := io.ReadAll(resp.Body)
		async_err.Err = fmt.Errorf("writing file in %s failed: %d - %s", persistentId, resp.StatusCode, string(b))
	}
}
