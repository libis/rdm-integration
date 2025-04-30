// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/core"
	dv "integration/app/plugin/impl/dataverse"
	"integration/app/plugin/types"
	"io"
	"net/http"

	"github.com/libis/rdm-dataverse-go-api/api"
)

func GetMetadata(w http.ResponseWriter, r *http.Request) {
	req := types.GetMetadataRequest{}
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		return
	}

	user := core.GetUserFromHeader(r.Header)
	metadata, err := getMetadata(r.Context(), req, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - matadata retrieval failed"))
		return
	}

	b, err = json.Marshal(metadata)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func getMetadata(ctx context.Context, getMetadataRequest types.GetMetadataRequest, user string) (types.Metadata, error) {
	if getMetadataRequest.Plugin != "dataverse" {
		return nil, nil
	}
	from := "/api/v1/datasets/:persistentId/versions/:latest?persistentId=" + getMetadataRequest.RepoName
	fromClient := dv.NewClient(getMetadataRequest.PluginId, getMetadataRequest.Url, user, getMetadataRequest.Token)
	fromRequest := fromClient.NewRequest(from, "GET", nil, nil)
	res := map[string]interface{}{}
	err := api.Do(ctx, fromRequest, &res)
	if err != nil {
		return nil, err
	}
	if res["status"] != "OK" {
		return nil, fmt.Errorf("metadata copy failed: %v", res["message"])
	}
	metadataBlocks := map[string]interface{}{}
	metadataBlocks["metadataBlocks"] = res["data"].(map[string]interface{})["metadataBlocks"]
	return types.Metadata{"datasetVersion": metadataBlocks}, nil
}
