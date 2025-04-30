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

type Metadata map[string]interface{}

func copyMetaData(compareRequest types.CompareRequest, user string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data, err := getMetadata(ctx, compareRequest, user)
	if err != nil {
		return err
	}
	return putMetadata(ctx, compareRequest, user, data)
}

func getMetadata(ctx context.Context, compareRequest types.CompareRequest, user string) (Metadata, error) {
	from := "/api/v1/datasets/:persistentId/versions/:latest?persistentId=" + compareRequest.RepoName
	fromClient := dv.NewClient(compareRequest.PluginId, compareRequest.Url, user, compareRequest.Token)
	fromRequest := fromClient.NewRequest(from, "GET", nil, nil)
	res := map[string]interface{}{}
	err := api.Do(ctx, fromRequest, &res)
	if err != nil {
		return nil, err
	}
	if res["status"] != "OK" {
		return nil, fmt.Errorf("metadata copy failed: %v", res["message"])
	}
	metadataBlocks := Metadata{}
	metadataBlocks["metadataBlocks"] = res["data"].(map[string]interface{})["metadataBlocks"]
	return metadataBlocks, nil
}

func putMetadata(ctx context.Context, compareRequest types.CompareRequest, user string, data Metadata) error {
	to := "/api/v1/datasets/:persistentId/versions/:draft?persistentId=" + compareRequest.PersistentId
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	toReq := dataverse.GetRequest(to, "PUT", user, compareRequest.DataverseKey, bytes.NewBuffer(dataBytes), api.JsonContentHeader())
	res := map[string]interface{}{}
	err = api.Do(ctx, toReq, &res)
	if err != nil {
		return err
	}
	if res["status"] != "OK" {
		return fmt.Errorf("metadata copy failed: %v", res["message"])
	}
	return nil
}
