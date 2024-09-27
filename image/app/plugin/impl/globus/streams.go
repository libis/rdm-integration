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

	"github.com/libis/rdm-dataverse-go-api/api"
)

type UserInfo struct {
	Principal string `json:"sub"`
}

type SubmissionId struct {
	Value string `json:"value"`
}

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	token := streamParams.Token
	if token == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: token")
	}

	return types.StreamsType{Streams: nil, Cleanup: func() error {
		info, err := getUserInfo(ctx, token)
		if err != nil {
			return err
		}
		paths, err := RequestGlobusUploadPaths(ctx, streamParams.PersistentId, streamParams.DVToken, streamParams.User, info.Principal, len(in))
		if err != nil {
			return err
		}
		submissionId, err := getSubmissionId(ctx, token)
		if err != nil {
			return err
		}
		//TODO: transfer
		//TODO: addGlobusFiles
		/* examples:
-> POST https://transfer.api.globusonline.org/v0.10/transfer
{"DATA_TYPE":"transfer","DATA":[{"DATA_TYPE":"transfer_item","source_path":"/~/Downloads/authentication.drawio","destination_path":"/10.5072/FK2/TO4LCH/19233b19b57-d9056576dc22","recursive":false},{"DATA_TYPE":"transfer_item","source_path":"/~/Downloads/authentication.svg","destination_path":"/10.5072/FK2/TO4LCH/19233b19b57-f4c99a1f5c55","recursive":false}],"submission_id":"80862113-7cd5-11ef-b6cd-6d7d1acfb36d","notify_on_succeeded":false,"notify_on_failed":false,"source_endpoint":"3ac22a16-70e8-11ef-b4ae-8fef73a45f39","destination_endpoint":"f242e39a-3204-4f6e-aa4c-25be857d731d"}


{
  "DATA_TYPE": "transfer_result",
  "code": "Accepted",
  "message": "The transfer has been accepted and a task has been created and queued for execution",
  "request_id": "SgNsoHlq2",
  "resource": "/transfer",
  "submission_id": "80862113-7cd5-11ef-b6cd-6d7d1acfb36d",
  "task_id": "80862112-7cd5-11ef-b6cd-6d7d1acfb36d",
  "task_link": {
    "DATA_TYPE": "link",
    "href": "task/80862112-7cd5-11ef-b6cd-6d7d1acfb36d?format=json",
    "rel": "related",
    "resource": "task",
    "title": "related task"
  }
}


-> POST https://localhost:7000/api/v1/datasets/41/addGlobusFiles
{ "taskIdentifier": "80862112-7cd5-11ef-b6cd-6d7d1acfb36d","files":[{ "description": "", "directoryLabel": "", "restrict": "false","storageIdentifier":"globus://19233b19b57-d9056576dc22","fileName":"authentication.drawio" } ,{ "description": "", "directoryLabel": "", "restrict": "false","storageIdentifier":"globus://19233b19b57-f4c99a1f5c55","fileName":"authentication.svg" } ]}

{"status":"OK","data":{"message":"Async call to Globus Upload started "}}
		*/
		fmt.Println(paths)
		fmt.Println(submissionId.Value)
		return nil
	}}, nil
}

func getUserInfo(ctx context.Context, token string) (UserInfo, error) {
	b, err := DoGlobusRequest(ctx, "https://auth.globus.org/v2/oauth2/userinfo", "GET", token, nil)
	if err != nil {
		return UserInfo{}, err
	}
	response := UserInfo{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return UserInfo{}, fmt.Errorf("globus error: UserInfo could not be unmarshalled from %v", string(b))
	}
	return response, nil
}

func getSubmissionId(ctx context.Context, token string) (SubmissionId, error) {
	b, err := DoGlobusRequest(ctx, "https://transfer.api.globusonline.org/v0.10/submission_id", "GET", token, nil)
	if err != nil {
		return SubmissionId{}, err
	}
	response := SubmissionId{}
	err = json.Unmarshal(b, &response)
	if err != nil {
		return SubmissionId{}, fmt.Errorf("globus error: SubmissionId could not be unmarshalled from %v", string(b))
	}
	return response, nil
}

func RequestGlobusUploadPaths(ctx context.Context, persistentId, token, user, principal string, nbFiles int) (map[string]string, error) {
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
		return nil, fmt.Errorf("requesting Globus upload paths failed: %s", res.Message)
	}
	return res.Data, nil
}
