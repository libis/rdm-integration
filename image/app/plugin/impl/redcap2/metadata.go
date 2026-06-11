// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"strings"
)

// Metadata maps REDCap project information onto the Dataverse citation
// metadata used to prefill a new dataset: title, description (project notes),
// principal investigator, grant number, IRB number and the REDCap project id.
func Metadata(ctx context.Context, streamParams types.StreamParams) (types.MetadataStruct, error) {
	if streamParams.Url == "" || streamParams.Token == "" {
		return types.MetadataStruct{}, fmt.Errorf("metadata: missing parameters: expected url, token")
	}

	payload, _, err := exportProjectInfo(ctx, streamParams.Url, streamParams.Token)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	info := projectInfoMap(payload)

	res := types.MetadataStruct{
		Title: stringField(info, "project_title"),
	}

	if notes := stringField(info, "project_notes"); notes != "" {
		res.DsDescription = append(res.DsDescription, notes)
	}
	if purpose := stringField(info, "purpose_other"); purpose != "" {
		res.DsDescription = append(res.DsDescription, "Purpose: "+purpose)
	}

	firstName := stringField(info, "project_pi_firstname")
	lastName := stringField(info, "project_pi_lastname")
	if piName := citationName(firstName, lastName); piName != "" {
		res.Author = []types.Author{{AuthorName: piName}}
	}

	if grant := stringField(info, "project_grant_number"); grant != "" {
		res.GrantNumber = []types.GrantNumber{{GrantNumberValue: grant}}
	}

	if irb := stringField(info, "project_irb_number"); irb != "" {
		res.OtherId = append(res.OtherId, types.OtherId{OtherIdAgency: "IRB", OtherIdValue: irb})
	}
	if id, ok := info["project_id"]; ok && id != nil {
		res.OtherId = append(res.OtherId, types.OtherId{
			OtherIdAgency: "REDCap",
			OtherIdValue:  fmt.Sprintf("urn:redcap:%s:project:%v", streamParams.Url, id),
		})
	}

	return res, nil
}

// projectInfoMap parses a content=project JSON payload (object or
// single-element array form) into a generic map.
func projectInfoMap(payload []byte) map[string]interface{} {
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err == nil {
		return obj
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(payload, &arr); err == nil && len(arr) > 0 {
		return arr[0]
	}
	return map[string]interface{}{}
}

func stringField(info map[string]interface{}, key string) string {
	if s, ok := info[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// citationName renders "Family, Given" with either part optional.
func citationName(firstName, lastName string) string {
	switch {
	case firstName == "":
		return lastName
	case lastName == "":
		return firstName
	}
	return lastName + ", " + firstName
}
