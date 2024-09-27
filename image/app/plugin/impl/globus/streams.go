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
