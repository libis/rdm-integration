// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package common

import (
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"integration/app/dataverse"
	"integration/app/tree"
	"io"
	"net/http"
	"strings"
)

func GetDownloadableFiles(w http.ResponseWriter, r *http.Request) {
	if !config.RedisReady(r.Context()) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
	req := CompareRequest{}
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

	//get files and write response
	user := core.GetUserFromHeader(r.Header)
	nm, hasRestricted, hasEmbargoed, err := dataverse.GetDatasetNodesWithAccessInfo(r.Context(), req.PersistentId, req.DataverseKey, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - getting files failed"))
		return
	}

	// Check if user can download all files in this dataset.
	// We only proceed if the user has access to all files to avoid partial/failed transfers.
	canDownloadAll, err := dataverse.CanUserDownloadAllFiles(r.Context(), req.PersistentId, req.DataverseKey, user, hasRestricted, hasEmbargoed)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - checking permissions failed"))
		return
	}
	if !canDownloadAll {
		reasons := []string{}
		if hasRestricted {
			reasons = append(reasons, "restricted files")
		}
		if hasEmbargoed {
			reasons = append(reasons, "an active embargo")
		}
		reasonText := "restricted files or an active embargo"
		if len(reasons) == 1 {
			reasonText = reasons[0]
		} else if len(reasons) == 2 {
			reasonText = strings.Join(reasons, " and ")
		}
		w.WriteHeader(http.StatusForbidden)
		// Provide a more helpful message for guests vs logged-in users
		var helpText string
		if user == "" {
			helpText = "If you have access, please log in first."
		} else {
			helpText = "Dataset owners and curators can download restricted content."
		}
		w.Write([]byte(fmt.Sprintf("403 - dataset cannot be downloaded because it has %s and you do not have permission to access all files. %s", reasonText, helpText)))
		return
	}
	data := []tree.Node{}
	for _, node := range nm {
		if node.Attributes.IsFile {
			data = append(data, node)
		}
	}
	res := core.CompareResponse{
		Id:     req.PersistentId,
		Status: core.Finished,
		Data:   data,
		Url:    core.Destination.GetRepoUrl(req.PersistentId, false),
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
