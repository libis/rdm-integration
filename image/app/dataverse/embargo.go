// Author: Eryk Kulikowski @ KU Leuven (2025). Apache 2.0 License

package dataverse

import (
	"context"
	"fmt"
	"integration/app/core"
	"integration/app/logging"
	"integration/app/tree"
	"strings"
	"time"

	"github.com/libis/rdm-dataverse-go-api/api"
)

var embargoDateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

type datasetFilesResponse struct {
	Status  string             `json:"status"`
	Message string             `json:"message,omitempty"`
	Data    []datasetFileEntry `json:"data"`
}

type datasetFileEntry struct {
	api.MetaData
	Embargo *datasetEmbargo `json:"embargo"`
}

type datasetEmbargo struct {
	DateAvailable string `json:"dateAvailable"`
	Reason        string `json:"reason,omitempty"`
}

type datasetVersionDetailResponse struct {
	Status  string                   `json:"status"`
	Message string                   `json:"message,omitempty"`
	Data    datasetVersionDetailData `json:"data"`
}

type datasetVersionDetailData struct {
	DatasetVersion datasetVersionMetadata `json:"datasetVersion"`
}

type datasetVersionMetadata struct {
	MetadataBlocks map[string]datasetMetadataBlock `json:"metadataBlocks"`
}

type datasetMetadataBlock struct {
	Fields []datasetMetadataField `json:"fields"`
}

type datasetMetadataField struct {
	TypeName string `json:"typeName"`
	Value    any    `json:"value"`
}

func GetDatasetNodesWithAccessInfo(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, bool, bool, error) {
	mapped, hasRestricted, hasEmbargoed, err := datasetNodesWithAccessInfo(ctx, persistentId, token, user)
	if err != nil {
		return nil, false, false, err
	}
	core.CheckKnownHashes(ctx, persistentId, mapped)
	return mapped, hasRestricted, hasEmbargoed, nil
}

func datasetNodesWithAccessInfo(ctx context.Context, persistentId, token, user string) (map[string]tree.Node, bool, bool, error) {
	entries, err := fetchDatasetFileEntries(ctx, persistentId, token, user)
	if err != nil {
		return nil, false, false, err
	}
	metadata := make([]api.MetaData, len(entries))
	hasRestricted := false
	hasEmbargoed := false
	now := time.Now().UTC()
	for i, entry := range entries {
		metadata[i] = entry.MetaData
		if entry.Restricted {
			hasRestricted = true
		}
		if entry.Embargo != nil {
			active, parseErr := isEmbargoActive(entry.Embargo.DateAvailable, now)
			if parseErr != nil {
				logging.Logger.Printf("failed to parse embargo date %q for dataset %s: %v", entry.Embargo.DateAvailable, persistentId, parseErr)
				hasEmbargoed = true
			} else if active {
				hasEmbargoed = true
			}
		}
	}
	if embargoDate, err := fetchDatasetEmbargoDate(ctx, persistentId, token, user); err != nil {
		logging.Logger.Printf("failed to fetch dataset-level embargo for %s: %v", persistentId, err)
	} else if embargoDate != "" {
		active, parseErr := isEmbargoActive(embargoDate, now)
		if parseErr != nil {
			logging.Logger.Printf("failed to parse dataset-level embargo date %q for dataset %s: %v", embargoDate, persistentId, parseErr)
			hasEmbargoed = true
		} else if active {
			hasEmbargoed = true
		}
	}
	return mapToNodes(metadata), hasRestricted, hasEmbargoed, nil
}

func fetchDatasetFileEntries(ctx context.Context, persistentId, token, user string) ([]datasetFileEntry, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	res := datasetFilesResponse{}
	req := GetRequest(datasetFilesPath(persistentId), "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "OK" {
		message := res.Message
		if message == "" {
			message = "no additional details"
		}
		return nil, fmt.Errorf("listing files for %s failed: status=%s (%s)", persistentId, res.Status, message)
	}
	return res.Data, nil
}

func datasetFilesPath(persistentId string) string {
	return "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
}

func fetchDatasetEmbargoDate(ctx context.Context, persistentId, token, user string) (string, error) {
	shortContext, cancel := context.WithTimeout(ctx, dvContextDuration)
	defer cancel()
	res := datasetVersionDetailResponse{}
	req := GetRequest(datasetVersionPath(persistentId), "GET", user, token, nil, nil)
	err := api.Do(shortContext, req, &res)
	if err != nil {
		return "", err
	}
	if res.Status != "OK" {
		message := res.Message
		if message == "" {
			message = "no additional details"
		}
		return "", fmt.Errorf("fetching dataset version for %s failed: status=%s (%s)", persistentId, res.Status, message)
	}
	if res.Data.DatasetVersion.MetadataBlocks == nil {
		return "", nil
	}
	return extractEmbargoDate(res.Data.DatasetVersion.MetadataBlocks), nil
}

func datasetVersionPath(persistentId string) string {
	return "/api/v1/datasets/:persistentId/versions/:latest?persistentId=" + persistentId + "&excludeFiles=true"
}

func extractEmbargoDate(blocks map[string]datasetMetadataBlock) string {
	for _, block := range blocks {
		if date := extractEmbargoDateFromFields(block.Fields); date != "" {
			return date
		}
	}
	return ""
}

func extractEmbargoDateFromFields(fields []datasetMetadataField) string {
	for _, field := range fields {
		if strings.EqualFold(field.TypeName, "embargoDate") {
			return normalizeEmbargoValue(field.Value)
		}
	}
	return ""
}

func normalizeEmbargoValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		for _, item := range v {
			if result := normalizeEmbargoValue(item); result != "" {
				return result
			}
		}
	case map[string]interface{}:
		if nested, ok := v["value"]; ok {
			if result := normalizeEmbargoValue(nested); result != "" {
				return result
			}
		}
	}
	return ""
}

func isEmbargoActive(dateString string, now time.Time) (bool, error) {
	if dateString == "" {
		return false, nil
	}
	parsed, err := parseEmbargoDate(dateString)
	if err != nil {
		return false, err
	}
	return parsed.After(now), nil
}

func parseEmbargoDate(value string) (time.Time, error) {
	for _, layout := range embargoDateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported embargo date format %q", value)
}
