// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/dataverse"
	dv "integration/app/plugin/impl/dataverse"
	"integration/app/plugin/types"
	"time"

	"github.com/libis/rdm-dataverse-go-api/api"
)

func copyMetaData(compareRequest types.CompareRequest, user string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data, err := getMetadata(ctx, compareRequest, user)
	if err != nil {
		return err
	}
	return putMetadata(ctx, compareRequest, user, data)
}

func getMetadata(ctx context.Context, compareRequest types.CompareRequest, user string) ([]byte, error) {
	from := "/api/v1/datasets/:persistentId/versions/:latest?persistentId=" + compareRequest.RepoName
	fromClient, _ := dv.NewClient(compareRequest.PluginId, compareRequest.Url, user, compareRequest.Token)
	fromRequest := fromClient.NewRequest(from, "GET", nil, nil)
	md := map[string]interface{}{}
	err := api.Do(ctx, fromRequest, &md)
	if err != nil {
		return nil, err
	}
	if md["status"] != "OK" {
		return nil, fmt.Errorf("metadata copy failed: %v", md["message"])
	}
	metadataBlocks := map[string]interface{}{}
	metadataBlocks["metadataBlocks"] = md["data"].(map[string]interface{})["metadataBlocks"]
	return json.Marshal(metadataBlocks)
}

func putMetadata(ctx context.Context, compareRequest types.CompareRequest, user string, data []byte) error {
	to := "/api/v1/datasets/:persistentId/versions/:draft?persistentId=" + compareRequest.PersistentId
	toReq := dataverse.GetRequest(to, "PUT", user, compareRequest.DataverseKey, bytes.NewBuffer(data), api.JsonContentHeader())
	res := map[string]interface{}{}
	err := api.Do(ctx, toReq, &res)
	if err != nil {
		return err
	}
	if res["status"] != "OK" {
		return fmt.Errorf("metadata copy failed: %v", res["message"])
	}
	return nil
}
