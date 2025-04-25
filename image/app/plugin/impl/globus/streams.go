// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	coreTypes "integration/app/core/types"
	"integration/app/logging"
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
	DataType     string   `json:"DATA_TYPE"`
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	RequestId    string   `json:"request_id"`
	Resource     string   `json:"resource"`
	SubmissionId string   `json:"submission_id"`
	TaskId       string   `json:"task_id"`
	TaskLink     TaskLink `json:"task_link"`
}

type TaskLink struct {
	DataType string `json:"DATA_TYPE"`
	Href     string `json:"href"`
	Rel      string `json:"rel"`
	Resource string `json:"resource"`
	Title    string `json:"title"`
}

type AddGlobusFilesRequest struct {
	TaskIdentifier string `json:"taskIdentifier"`
	Files          []File `json:"files"`
}

type File struct {
	Description       string   `json:"description"`
	DirectoryLabel    string   `json:"directoryLabel"`
	Categories        []string `json:"categories"`
	Restrict          bool     `json:"restrict"`
	StorageIdentifier string   `json:"storageIdentifier"`
	FileName          string   `json:"fileName"`
	MimeType          string   `json:"mimeType"`
	Checksum          Checksum `json:"checksum"`
}

type Checksum struct {
	Type  string `json:"@type"`
	Value string `json:"@value"`
}

type Path struct {
	Id   string
	Path string
}

func Streams(ctx context.Context, in map[string]tree.Node, p types.StreamParams) (types.StreamsType, error) {
	if len(in) == 0 {
		return types.StreamsType{}, nil
	}
	sessionId, token, repoName, option, pId, dvToken, user := p.SessionId, p.Token, p.RepoName, p.Option, p.PersistentId, p.DVToken, p.User
	if token == "" || repoName == "" || option == "" {
		return types.StreamsType{}, fmt.Errorf("globus streams: missing parameters")
	}
	return types.StreamsType{Streams: nil, Cleanup: func() error {
		err := doTransfer(ctx, sessionId, token, repoName, option, pId, dvToken, user, in)
		if err != nil {
			logging.Logger.Println("globus transfer failed: " + err.Error())
		}
		return err
	}}, nil
}

func doTransfer(ctx context.Context, sessionId, token, repoName, option, pId, dvToken, user string, in map[string]tree.Node) error {
	destinationEndpoint, err := getDestinationEndpoint(ctx, pId, dvToken, user)
	if err != nil {
		return err
	}
	prinicpal, err := getPrincipal(ctx, sessionId)
	if err != nil {
		return err
	}
	paths, err := RequestGlobusUploadPaths(ctx, pId, dvToken, user, prinicpal, len(in))
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
		SourceEndpoint:      repoName,
		DestinationEndpoint: destinationEndpoint,
	}
	addGlobusFilesRequest := AddGlobusFilesRequest{}
	index := 0
	for k, v := range in {
		transferRequest.Data = append(transferRequest.Data, TransferRequestData{
			DataType:        "transfer_item",
			SourcePath:      option + "/" + k,
			DestinationPath: paths[index].Path,
			Recursive:       false,
		})
		addGlobusFilesRequest.Files = append(addGlobusFilesRequest.Files, File{
			Description:       "",
			DirectoryLabel:    v.Path,
			Categories:        nil,
			Restrict:          false,
			StorageIdentifier: paths[index].Id,
			FileName:          v.Name,
			MimeType:          "application/octet-stream",
			Checksum: Checksum{
				Type:  v.Attributes.RemoteHashType,
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
	return addGlobusFiles(ctx, pId, dvToken, user, addGlobusFilesRequest)
}

func getPrincipal(ctx context.Context, sessionId string) (string, error) {
	token, ok := getTokenFromCache(ctx, sessionId)
	if !ok {
		return "", fmt.Errorf("globus error: token not in cache")
	}
	b, err := DoGlobusRequest(ctx, "https://auth.globus.org/v2/oauth2/userinfo", "GET", token.AccessToken, nil)
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
	if response.Code != "Accepted" || response.TaskId == "" {
		return "", fmt.Errorf("globus error: transfer failed to start: %v", string(b))
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

func getTokenFromCache(ctx context.Context, sessionId string) (coreTypes.OauthTokenResponse, bool) {
	cached := config.GetRedis().Get(ctx, fmt.Sprintf("%v-%v", "globus", sessionId))
	jsonString := cached.Val()
	if jsonString == "" {
		return coreTypes.OauthTokenResponse{}, false
	}
	res := coreTypes.OauthTokenResponse{}
	json.Unmarshal([]byte(jsonString), &res)
	return res, true
}

func Download(ctx context.Context, p types.StreamParams, in map[string]tree.Node) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	sessionId, token, repoName, option, pId, dvToken, user, _ := p.SessionId, p.Token, p.RepoName, p.Option, p.PersistentId, p.DVToken, p.User, p.DownloadId
	if token == "" || repoName == "" || option == "" {
		return "", fmt.Errorf("globus download: missing parameters")
	}
	sourceEndpoint, err := getDestinationEndpoint(ctx, pId, dvToken, user)
	if err != nil {
		return "", err
	}
	prinicpal, err := getPrincipal(ctx, sessionId)
	if err != nil {
		return "", err
	}
	fileIds := []int64{}
	for _, f := range in {
		fileIds = append(fileIds, f.Attributes.DestinationFile.Id)
	}
	paths, err := requestGlobusDownload(ctx, pId, dvToken, user, prinicpal, fileIds)
	if err != nil {
		return "", err
	}
	submissionId, err := getSubmissionId(ctx, token)
	if err != nil {
		return "", err
	}
	transferRequest := TransferRequest{
		DataType:            "transfer",
		SubmissionId:        submissionId,
		NotifyOnSucceeded:   false,
		NotifyOnFailed:      false,
		SourceEndpoint:      sourceEndpoint,
		DestinationEndpoint: repoName,
	}
	for k, f := range in {
		sourcePath, ok := paths[fmt.Sprint(f.Attributes.DestinationFile.Id)]
		if !ok {
			return "", fmt.Errorf("globus download path for %v unknown", k)
		}
		transferRequest.Data = append(transferRequest.Data, TransferRequestData{
			DataType:        "transfer_item",
			SourcePath:      sourcePath,
			DestinationPath: option + "/" + k,
			Recursive:       false,
		})
	}
	return transfer(ctx, token, transferRequest)
}

func RequestGlobusDownloadParameters(ctx context.Context, persistentId, token, user, downloadId string) (string, error) {
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/globusDownloadParameters?persistentId=" + persistentId + "&downloadId" + downloadId
	client := api.NewUrlSigningClient(config.GetConfig().DataverseServer, user, config.ApiKey, config.UnblockKey)
	client.Token = token
	req := client.NewRequest(path, "GET", nil, api.JsonContentHeader())
	res := map[string]interface{}{}
	err := api.Do(ctx, req, &res)
	if err != nil {
		return "", err
	}
	data, ok := res["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("globus error: destination endpoint id not found in %+v", res)
	}
	queryParameters, ok := data["queryParameters"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("globus error: destination endpoint id not found in %+v", res)
	}
	sourceEndpoint, ok := queryParameters["endpoint"].(string)
	if !ok || sourceEndpoint == "" {
		return "", fmt.Errorf("globus error: source endpoint id not found in %+v", res)
	}
	return sourceEndpoint, nil
}

func requestGlobusDownload(ctx context.Context, persistentId, token, user, principal string, fileIds []int64) (map[string]string, error) {
	path := config.GetConfig().DataverseServer + "/api/v1/datasets/:persistentId/requestGlobusDownload?persistentId=" + persistentId
	data, _ := json.Marshal(map[string]interface{}{"principal": principal, "fileIds": fileIds})
	client := api.NewUrlSigningClient(config.GetConfig().DataverseServer, user, config.ApiKey, config.UnblockKey)
	client.Token = token
	req := client.NewRequest(path, "POST", bytes.NewReader(data), api.JsonContentHeader())
	res := map[string]interface{}{}
	err := api.Do(ctx, req, &res)
	if err != nil {
		return nil, err
	}
	receivedData, ok := res["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("requesting Globus download paths failed: %+v", res)
	}
	logging.Logger.Printf("\nGlobus download parameters response:\n\n%+v\n\n", res)
	params := map[string]string{}
	for k, v := range receivedData {
		str, ok := v.(string)
		if ok {
			params[k] = str
		}
	}
	return params, nil
}
