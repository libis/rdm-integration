package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/dv"
	"integration/app/logging"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
)

func GetNodeMap(doi, token string) (map[string]tree.Node, error) {
	url := dataverseServer + "/api/datasets/:persistentId/versions/:latest/files?persistentId=doi:" + doi
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
		return nil, fmt.Errorf("listing files for %s failed: %+v", doi, res)
	}
	return mapToNodes(res.Data), nil
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
			Html: d.DataFile.Filename,
			Attributes: tree.Attributes{
				ParentId:  d.DirectoryLabel,
				Metadata:  d,
				IsFile:    true,
				LocalHash: d.DataFile.Checksum.Value,
			},
			Checked: true,
		}
	}
	return res
}

func PersistNodeMap(job Job) error {
	ctx := context.Background()
	streams, err := deserialize(ctx, job.StreamType, job.Streams, job.StreamParams)
	if err != nil {
		return err
	}
	return doPersistNodeMap(ctx, job.DataverseKey, job.Doi, job.WritableNodes, streams, job)
}

var stopped = fmt.Errorf("stopped")

func doPersistNodeMap(ctx context.Context, dataverseKey, doi string, writableNodes map[string]tree.Node, streams map[string]Stream, job Job) (err error) {
	err = checkPermission(dataverseKey, doi)
	if err != nil {
		logging.Logger.Println(err)
		//TODO
		//return err
		err = nil
	}
	knownHashes := getKnownHashes(doi)
	defer func() {
		storeKnownHashes(doi, knownHashes)
		if len(job.WritableNodes) > 0 {
			unlock(doi)
			err = AddJob(job)
		}
	}()
	for k, v := range writableNodes {
		select {
		case <-Stop:
			return
		default:
		}

		if !v.Checked && v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, doi, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return
			}
			delete(knownHashes, v.Id)
			continue
		}
		stream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := defaultHash
		remoteHashType := v.Attributes.RemoteHashType
		var h []byte
		var remoteH []byte
		h, remoteH, err = write(stream, storageIdentifier, doi, hashType, remoteHashType, v.Attributes.Metadata.DataFile.Filesize)
		if err == stopped {
			return nil
		}
		if err != nil {
			return
		}
		hashValue := fmt.Sprintf("%x", h)
		knownHashes[v.Id] = CalculatedHashes{
			LocalHashType:  hashType,
			LocalHashValue: hashValue,
			RemoteHashes:   map[string]string{remoteHashType: fmt.Sprintf("%x", remoteH)},
		}
		if v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, doi, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return err
			}
		}
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
		err = writeToDV(dataverseKey, doi, data)
		if err != nil {
			return err
		}
		delete(job.WritableNodes, k)
	}
	return nil
}

func deleteFromDV(dataverseKey, doi string, id int) error {
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", dataverseServer, id)
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

func writeToDV(dataverseKey, doi string, jsonData dv.JsonData) error {
	url := dataverseServer + "/api/datasets/:persistentId/addFiles?persistentId=doi:" + doi
	data, err := json.Marshal([]dv.JsonData{jsonData})
	if err != nil {
		return err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormField("jsonData")
	if err != nil {
		return err
	}
	part.Write(data)
	writer.Close()
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Add("Content-Type", writer.FormDataContentType())
	request.Header.Add("X-Dataverse-key", dataverseKey)
	r, err := http.DefaultClient.Do(request)
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("writing file in %s failed: %d - %s", doi, r.StatusCode, string(b))
	}
	return err
}

// TODO: fixme
func checkPermission(dataverseKey, doi string) error {
	url := fmt.Sprintf("%s/api/admin/permissions/%v?unblock-key=%v", dataverseServer, url.PathEscape("doi:"+doi), unblockKey)
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
		return fmt.Errorf("getting permissions for dataset %s failed: %s", doi, string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	logging.Logger.Println(string(b))
	return err
}
