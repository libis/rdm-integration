// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	dv "integration/app/dataverse"
	"integration/app/logging"
	"integration/app/plugin/funcs/stream"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
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
	responseData, err := ioutil.ReadAll(response.Body)
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

func doWork(job Job) (Job, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-Stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if job.Plugin == "hash-only" {
		return doRehash(ctx, job.DataverseKey, job.PersistentId, job.WritableNodes, job)
	}
	streams, err := stream.Streams(ctx, job.WritableNodes, job.Plugin, job.StreamParams)
	if err != nil {
		return job, err
	}
	knownHashes := getKnownHashes(job.PersistentId)
	//filter not valid actions (when someone had browser open for a very long time and other job started and finished)
	writableNodes, err := filterRedundant(job, knownHashes)
	if err != nil {
		return job, err
	}
	job.WritableNodes = writableNodes
	return doPersistNodeMap(ctx, streams, job, knownHashes)
}

func filterRedundant(job Job, knownHashes map[string]calculatedHashes) (map[string]tree.Node, error) {
	filteredEqual := map[string]tree.Node{}
	isDelete := false
	for k, v := range job.WritableNodes {
		localHash := knownHashes[k].LocalHashValue
		h, ok := knownHashes[k].RemoteHashes[v.Attributes.RemoteHashType]
		if v.Action == tree.Delete {
			isDelete = true
		} else if ok && h == v.Attributes.RemoteHash && localHash == v.Attributes.LocalHash {
			continue
		}
		filteredEqual[k] = v
	}
	if !isDelete {
		return filteredEqual, nil
	}
	res := map[string]tree.Node{}
	nm, err := GetNodeMap(job.PersistentId, job.DataverseKey)
	if err != nil {
		return nil, err
	}
	for k, v := range filteredEqual {
		_, ok := nm[k]
		if v.Action == tree.Delete && !ok {
			continue
		}
		res[k] = v
	}
	return res, nil
}

func doPersistNodeMap(ctx context.Context, streams map[string]types.Stream, in Job, knownHashes map[string]calculatedHashes) (out Job, err error) {
	dataverseKey, persistentId, writableNodes := in.DataverseKey, in.PersistentId, in.WritableNodes
	err = CheckPermission(dataverseKey, persistentId)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			storeKnownHashes(persistentId, knownHashes)
		}
	}()
	out = in
	i := 0
	total := len(writableNodes)
	for k, v := range writableNodes {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}

		if v.Action == tree.Delete {
			err = deleteFromDV(dataverseKey, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return
			}
			delete(knownHashes, v.Id)
			delete(out.WritableNodes, k)
			GetRedis().Set(ctx, fmt.Sprintf("%v -> %v", persistentId, k), types.Deleted, time.Minute)
			continue
		}
		// delete previous version before writting new version when replacing
		if v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return
			}
		}
		fileStream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := config.Options.DefaultHash
		remoteHashType := v.Attributes.RemoteHashType
		var h []byte
		var remoteH []byte
		var size int
		h, remoteH, size, err = write(ctx, dataverseKey, fileStream, storageIdentifier, persistentId, hashType, remoteHashType, k, v.Attributes.Metadata.DataFile.Filesize)
		if err != nil {
			return
		}
		v.Attributes.Metadata.DataFile.Filesize = size;
		hashValue := fmt.Sprintf("%x", h)
		//updated or new: always rehash
		remoteHashVlaue := fmt.Sprintf("%x", remoteH)
		if remoteHashType == types.GitHash {
			remoteHashVlaue = v.Attributes.RemoteHash // gitlab does not provide filesize... If we do not know the filesize before calculating the hash, we can't calculate the git hash
		}
		if v.Attributes.RemoteHash != remoteHashVlaue && v.Attributes.RemoteHash != types.NotNeeded { // not all local file system hashes are calculated on beforehand (types.NotNeeded)
			err = fmt.Errorf("downloaded file hash not equal")
			return
		}

		if directUpload == "true" && config.Options.DefaultDriver != "" {
			directoryLabel := &(v.Attributes.Metadata.DirectoryLabel)
			if *directoryLabel == "" {
				directoryLabel = nil
			}
			data := dv.JsonData{
				StorageIdentifier: storageIdentifier,
				FileName:          v.Attributes.Metadata.DataFile.Filename,
				DirectoryLabel:    directoryLabel,
				MimeType:          "application/octet-stream",
				Checksum: dv.Checksum{
					Type:  hashType,
					Value: hashValue,
				},
			}
			err = writeToDV(dataverseKey, persistentId, data)
			if err != nil {
				return
			}
		} else {
			nm := map[string]tree.Node{}
			fileFound := false
			written := tree.Node{}
			for i := 0; !fileFound && i < 5; i++ {
				nm, err = GetNodeMap(persistentId, dataverseKey)
				if err != nil {
					return
				}
				written, fileFound = nm[v.Id]
				time.Sleep(3*time.Second)
			}
			if !fileFound {
				err = fmt.Errorf("file is written but not found back")
				return
			}
			v.Attributes.Metadata.DataFile.Id  = written.Attributes.Metadata.DataFile.Id;
		}

		newH := []byte{}
		newH, err = doHash(ctx, dataverseKey, persistentId, v)
		if err != nil {
			return
		}
		if v.Attributes.RemoteHash != fmt.Sprintf("%x", newH) {
			err = fmt.Errorf("written file hash not equal")
		}

		if hashValue != remoteHashVlaue {
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: v.Attributes.RemoteHash},
			}
		}
		GetRedis().Set(ctx, fmt.Sprintf("%v -> %v", persistentId, k), types.Written, time.Minute)

		delete(out.WritableNodes, k)
	}
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
		err = cleanup(in.DataverseKey, in.PersistentId)
	}
	return
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
	if r.StatusCode != 200 && r.StatusCode != 202 && r.StatusCode != 204 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("deleting file %d failed: %d - %s", id, r.StatusCode, string(b))
	}
	return err
}

func writeToDV(dataverseKey, persistentId string, jsonData dv.JsonData) error {
	url := config.DataverseServer + "/api/datasets/:persistentId/addFiles?persistentId=" + persistentId
	data, err := json.Marshal([]dv.JsonData{jsonData})
	if err != nil {
		return err
	}
	body, formDataContentType, err := dv.RequestBody(data)
	if err != nil {
		return err
	}
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Add("Content-Type", formDataContentType)
	request.Header.Add("X-Dataverse-key", dataverseKey)
	r, err := http.DefaultClient.Do(request)
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("writing file in %s failed: %d - %s", persistentId, r.StatusCode, string(b))
	}
	b, _ := io.ReadAll(r.Body)
	res := dv.AddFilesResponse{}
	err = json.Unmarshal(b, &res)
	if res.Data.Result.Added != 1 && len(res.Data.Files) == 1 {
		return fmt.Errorf(res.Data.Files[0].ErrorMessage)
	}
	return err
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
		return "", fmt.Errorf("Dataverse collection was not specified: unable to create a new dataset")
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
	responseData, err := ioutil.ReadAll(response.Body)
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
	response, _ := http.DefaultClient.Do(request)
	responseData, _ := ioutil.ReadAll(response.Body)
	json.Unmarshal(responseData, &res)
	id := res.Id
	if id == 0 {
		return "", fmt.Errorf("dataset %v not found", persistentId)
	}
	return fmt.Sprintf("%s/api/admin/permissions/%v?&unblock-key=%s", config.DataverseServer, id, unblockKey), nil
}

func swordAddFile(dataverseKey, persistentId string, pr io.Reader, wg *sync.WaitGroup, err error) {
	url := config.DataverseServer + "/dvn/api/data-deposit/v1.1/swordv2/edit-media/study/" + persistentId
	var request *http.Request
	request, err = http.NewRequest("POST", url, pr)
	if err != nil {
		return
	}
	request.Header.Add("Content-Type", "application/zip")
	request.Header.Add("Content-Disposition", "filename=example.zip")
	request.Header.Add("Packaging", "http://purl.org/net/sword/package/SimpleZip")
	request.SetBasicAuth(dataverseKey, "")
	var r *http.Response
	r, err = http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	if r.StatusCode != 201 {
		b, _ := io.ReadAll(r.Body)
		err = fmt.Errorf("writing file in %s failed: %d - %s", persistentId, r.StatusCode, string(b))
	}
	wg.Done()
	return
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
		responseData, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != 200 && response.StatusCode != 201 {
			return nil, fmt.Errorf("Listing %v objects failed: %v", objectType, string(responseData))
		}
		retrieveResponse := dv.RetrieveResponse{}
		err = json.Unmarshal(responseData, &retrieveResponse)
		if err != nil {
			return nil, err
		}
		if !retrieveResponse.Success {
			return nil, fmt.Errorf("Listing %v objects was not successful: %v", objectType, retrieveResponse.ErrorMessage)
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
