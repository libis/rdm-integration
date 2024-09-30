// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/libis/rdm-dataverse-go-api/api"
)

type UserInfo struct {
	Principal string `json:"sub"`
}

type SubmissionId struct {
	Value string `json:"value"`
}

type TransferRequest struct {
	DataType            string                `json:"DATA_TYPE"`
	Data                []TransferRequestData `json:"DATA"`
	SubmissionId        string                `json:"submission_id"`
	NotifyOnSucceeded   bool                  `json:"notify_on_succeeded"`
	NotifyOnFailed      bool                  `json:"notify_on_failed"`
	SourceEndpoint      string                `json:"source_endpoint"`
	DestinationEndpoint string                `json:"destination_endpoint"`
}

type TransferRequestData struct {
	DataType        string `json:"DATA_TYPE"`
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Recursive       bool   `json:"recursive"`
}

type TransferResponse struct {
	DataType string `json:"DATA_TYPE"`
	Code string `json:"code"` // "Accepted",
	Message string `json:"message"`
	RequestId string `json:"request_id"`
	Resource string `json:"resource"`
	SubmissionId string `json:"submission_id"`
	TaskId string `json:"task_id"`
	TaskLink TaskLink `json:"task_link"`
}

type TaskLink struct {
	DataType string `json:"DATA_TYPE"`
	Href string `json:"href"`
	Rel string `json:"rel"`
	Resource string `json:"resource"`
	Title string `json:"title"`
}

type AddGlobusFilesRequest struct {
	TaskIdentifier string `json:"taskIdentifier"`
	Files          []File `json:"files"`
}

type File struct {
	Description       string   `json:"description"`       // "My description."
	DirectoryLabel    string   `json:"directoryLabel"`    // "data/subdir1"
	Categories        []string `json:"categories"`        // ["Data"]
	Restrict          bool     `json:"restrict"`          // "false"
	StorageIdentifier string   `json:"storageIdentifier"` // "globusm://18b39722140-50eb7d3c5ece"
	FileName          string   `json:"fileName"`          // "file2.txt"
	MimeType          string   `json:"mimeType"`          // "text/plain"
	Checksum          Checksum `json:"checksum"`          // {"@type": "MD5", "@value": "2345"}
}

type Checksum struct {
	Type  string `json:"@type"`
	Value string `json:"@value"`
}

type Path struct {
	Id string
	Path string
}

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	token := streamParams.Token
	if token == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: token")
	}

	return types.StreamsType{Streams: nil, Cleanup: func() error {
		destinationEndpoint, err := getDestinationEndpoint(ctx, streamParams.PersistentId, streamParams.DVToken, streamParams.User)
		if err != nil {
			return err
		}
		prinicpal, err := getPrincipal(ctx, token)
		if err != nil {
			return err
		}
		paths, err := RequestGlobusUploadPaths(ctx, streamParams.PersistentId, streamParams.DVToken, streamParams.User, prinicpal, len(in))
		if err != nil {
			return err
		}
		submissionId, err := getSubmissionId(ctx, token)
		if err != nil {
			return err
		}
		transferRequest := TransferRequest{
			DataType:            "transfer",
			SubmissionId:        submissionId,
			NotifyOnSucceeded:   false,
			NotifyOnFailed:      false,
			SourceEndpoint:      streamParams.RepoName,
			DestinationEndpoint: destinationEndpoint,
		}
		addGlobusFilesRequest := AddGlobusFilesRequest{}
		index := 0
		for k, v := range in {
			transferRequest.Data = append(transferRequest.Data, TransferRequestData{
				DataType:        "transfer_item",
				SourcePath:      streamParams.Option + "/" + k,
				DestinationPath: paths[index].Path,
				Recursive:       false,
			})
			addGlobusFilesRequest.Files = append(addGlobusFilesRequest.Files, File{
				Description: "",
				DirectoryLabel: v.Path,
				Categories: nil,
				Restrict: false,
				StorageIdentifier: paths[index].Id,
				FileName: v.Name,
				MimeType: "application/octet-stream",
				Checksum: Checksum{
					Type: v.Attributes.RemoteHashType,
					Value: v.Attributes.RemoteHash,
				},
			})
			index += 1
		}
		taskId, err := transfer(ctx, token, transferRequest)
		if err != nil {
			return err
		}
		addGlobusFilesRequest.TaskIdentifier = taskId
		return addGlobusFiles(ctx, streamParams.PersistentId, streamParams.DVToken, streamParams.User, addGlobusFilesRequest)
	}}, nil
}

func getPrincipal(ctx context.Context, token string) (string, error) {
	b, err := DoGlobusRequest(ctx, "https://auth.globus.org/v2/oauth2/userinfo", "GET", token, nil)
	if err != nil {
		return "", err
	}
	response := UserInfo{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return "", fmt.Errorf("globus error: UserInfo could not be unmarshalled from %v", string(b))
	}
	if response.Principal == "" {
		return "", fmt.Errorf("globus error: principal not found in %v", string(b))
	}
	return response.Principal, nil
}

func getSubmissionId(ctx context.Context, token string) (string, error) {
	b, err := DoGlobusRequest(ctx, "https://transfer.api.globusonline.org/v0.10/submission_id", "GET", token, nil)
	if err != nil {
		return "", err
	}
	response := SubmissionId{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return "", fmt.Errorf("globus error: SubmissionId could not be unmarshalled from %v", string(b))
	}
	if response.Value == "" {
		return "", fmt.Errorf("globus error: submission id not found in %v", string(b))
	}
	return response.Value, nil
}

func transfer(ctx context.Context, token string, transferRequest TransferRequest) (string, error) {
	requestBytes, _ := json.Marshal(transferRequest)
	b, err := DoGlobusRequest(ctx, "https://transfer.api.globusonline.org/v0.10/transfer", "POST", token, bytes.NewBuffer(requestBytes))
	if err != nil {
		return "", err
	}
	response := TransferResponse{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return "", fmt.Errorf("globus error: transfer response could not be unmarshalled from %v", string(b))
	}
	if response.TaskId == "" {
		return "", fmt.Errorf("globus error: task id not found in %v", string(b))
	}
	return response.TaskId, nil
}

func getDestinationEndpoint(ctx context.Context, persistentId, token, user string) (string, error) {
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/globusUploadParameters?persistentId=" + persistentId
	client := api.NewUrlSigningClient(config.GetConfig().DataverseServer, user, config.ApiKey, config.UnblockKey)
	client.Token = token
	req := client.NewRequest(path, "GET", nil, api.JsonContentHeader())
	res := map[string]interface{}{}
	err := api.Do(ctx, req, &res)
	if err != nil {
		return "", err
	}
	if res["status"] != "OK" {
		return "", fmt.Errorf("requesting Globus upload parameters failed: %s", res["message"])
	}
	data, ok := res["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("globus error: destination endpoint id not found in %+v", res)
	}
	queryParameters, ok := data["queryParameters"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("globus error: destination endpoint id not found in %+v", res)
	}
	destinationEndpoint, ok := queryParameters["endpoint"].(string)
	if !ok || destinationEndpoint == "" {
		return "", fmt.Errorf("globus error: destination endpoint id not found in %+v", res)
	}
	return destinationEndpoint, nil
}

func RequestGlobusUploadPaths(ctx context.Context, persistentId, token, user, principal string, nbFiles int) ([]Path, error) {
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/requestGlobusUploadPaths?persistentId=" + persistentId
	data, _ := json.Marshal(map[string]interface{}{"principal": principal, "numberOfFiles": nbFiles})
	client := api.NewUrlSigningClient(config.GetConfig().DataverseServer, user, config.ApiKey, config.UnblockKey)
	client.Token = token
	req := client.NewRequest(path, "POST", bytes.NewReader(data), api.JsonContentHeader())
	type Response struct {
		api.DvResponse
		Data map[string]string
	}
	res := Response{}
	err := api.Do(ctx, req, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		return nil, fmt.Errorf("requesting Globus upload paths failed: %+v", res)
	}
	pathsWithIds := []Path{}
	for k, v := range res.Data {
		pathsWithIds = append(pathsWithIds, Path{Id: k, Path: v})
	}
	return pathsWithIds, nil
}

func addGlobusFiles(ctx context.Context, persistentId, token, user string, request AddGlobusFilesRequest) error {
	data, _ := json.Marshal(request)
	body, formDataContentType := requestBody(data)
	reqHeader := http.Header{}
	reqHeader.Add("Content-Type", formDataContentType)
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/addGlobusFiles?persistentId=" + persistentId
	client := api.NewUrlSigningClient(config.GetConfig().DataverseServer, user, config.ApiKey, config.UnblockKey)
	client.Token = token
	req := client.NewRequest(path, "POST", body, reqHeader)
	res := map[string]interface{}{}
	err := api.Do(ctx, req, &res)
	if err != nil {
		return err
	}
	if res["status"] != "OK" {
		return fmt.Errorf("globus error: adding globus files failed: %+v", res)
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
