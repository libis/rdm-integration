package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
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

type Stream struct {
	Open  func() io.Reader
	Close func() error
}

type ListResponse struct {
	Status  string          `json:"status"`
	Data    []tree.Metadata `json:"data"`
	Message string          `json:"message"`
}

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
	res := ListResponse{}
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

func PersistNodeMap(dataverseKey, doi string, writableNodes map[string]tree.Node, streams map[string]Stream) {
	//TODO: put in queue: redis keyvalue store https://hub.docker.com/_/redis
	ctx := context.Background()
	err := doPersistNodeMap(ctx, dataverseKey, doi, writableNodes, streams)
	if err != nil {
		logging.Logger.Println(err)
	}
}

func doPersistNodeMap(ctx context.Context, dataverseKey, doi string, writableNodes map[string]tree.Node, streams map[string]Stream) (err error) {
	err = checkPermission(dataverseKey, doi)
	if err != nil {
		return err
	}
	for k, v := range writableNodes {
		if !v.Checked && v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, doi, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				logging.Logger.Println(err)
				return err
			}
			continue
		}
		stream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := defaultHash
		h, err := write(stream, storageIdentifier, doi, hashType)
		if err != nil {
			return err
		}
		if v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, doi, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return err
			}
		}
		hashValue := fmt.Sprintf("%x", h)
		directoryLabel := &(v.Attributes.Metadata.DirectoryLabel)
		if *directoryLabel == "" {
			directoryLabel = nil
		}
		data := JsonData{
			StorageIdentifier: storageIdentifier,
			FileName:          v.Attributes.Metadata.DataFile.Filename,
			DirectoryLabel:    directoryLabel,
			MimeType:          "application/octet-stream",
			Checksum: Checksum{
				Type:  hashType,
				Value: hashValue,
			},
		}
		err = writeToDV(dataverseKey, doi, data)
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteFromDV(dataverseKey, doi string, id int) error {
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", dataverseServer, id)
	request, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
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

func writeToDV(dataverseKey, doi string, jsonData JsonData) error {
	url := dataverseServer + "/api/datasets/:persistentId/addFiles?persistentId=doi:" + doi
	data, err := json.Marshal([]JsonData{jsonData})
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

//TODO: fixme
func checkPermission(dataverseKey, doi string) error {
	url := fmt.Sprintf("%s/api/admin/permissions/%v?unblock-key=%v", dataverseServer, url.PathEscape("doi:" + doi), "N86aBF6MrY4HIEGF4MT0")
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	request.Header.Add("X-Dataverse-key", dataverseKey)
	r, err := http.DefaultClient.Do(request)
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
