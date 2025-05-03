// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package common

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/core"
	"integration/app/dataverse"
	dv "integration/app/plugin/impl/dataverse"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"text/template"

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
	if getMetadataRequest.Plugin == "dataverse" {
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
	userObj, err := dataverse.GetUser(ctx, getMetadataRequest.Token, user)
	if err != nil {
		return nil, err
	}
	md := types.MetadataStruct{Author: []types.Author{{AuthorName: fmt.Sprintf("%v, %v", userObj.Data.LastName, userObj.Data.FirstName)}}}

	//TODO
	// citation.cff --> (2) codemeta.json --> (3) ro-crate.json --> (4) inherente system metadata (via de source API)

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	err = metadataTemplate.Execute(writer, md)
	if err != nil {
		return nil, err
	}
	writer.Flush()
	res := types.Metadata{}
	err = json.Unmarshal(b.Bytes(), &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

var metadataTemplate = template.Must(template.New("metadata").Parse(`
{
    "datasetVersion": {
        "metadataBlocks": {
            "citation": {
                "displayName": "Citation Metadata",
                "name": "citation",
                "fields": [
                    {{ if (ne .Title "") }}{
                        "typeName": "title",
                        "multiple": false,
                        "typeClass": "primitive",
                        "value": "{{ .Title }}"
                    },{{ end }}{{ if .AlternativeTitle }}{
                        "typeName": "alternativeTitle",
                        "multiple": true,
                        "typeClass": "primitive",
                        "value": [
                            {{ range $index, $element := .AlternativeTitle }}{{ if $index }},
                            {{ end }}"{{ $element }}"{{ end }}
                        ]
                    },{{ end }}{{ if .AlternativeURL }}{
                        "typeName": "alternativeURL",
                        "multiple": true,
                        "typeClass": "primitive",
                        "value": [
                            {{ range $index, $element := .AlternativeURL }}{{ if $index }},
                            {{ end }}"{{ $element }}"{{ end }}
                        ]
                    },{{ end }}{{ if .OtherId }}{
                        "typeName": "otherId",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .OtherId }}{{ if $index }},
                            {{ end }}{
                                "otherIdAgency": {
                                    "typeName": "otherIdAgency",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.OtherIdAgency }}"
                                },
                                "otherIdValue": {
                                    "typeName": "otherIdValue",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.OtherIdValue }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{{ if .DsDescription }}{
                        "typeName": "dsDescription",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .DsDescription }}{{ if $index }},
                            {{ end }}{
                                "dsDescriptionValue": {
                                    "typeName": "dsDescriptionValue",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{{ if .Keyword }}{
                        "typeName": "keyword",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .Keyword }}{{ if $index }},
                            {{ end }}{
                                "keywordValue": {
                                    "typeName": "keywordValue",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{{ if .ContributorName }}{
                        "typeName": "contributor",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .ContributorName }}{{ if $index }},
                            {{ end }}{
                                "contributorType": {
                                    "typeName": "contributorType",
                                    "multiple": false,
                                    "typeClass": "controlledVocabulary",
                                    "value": "Other"
                                },
                                "contributorName": {
                                    "typeName": "contributorName",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{{ if .GrantNumber }}{
                        "typeName": "grantNumber",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .GrantNumber }}{{ if $index }},
                            {{ end }}{
                                "grantNumberAgency": {
                                    "typeName": "grantNumberAgency",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.GrantNumberAgency }}"
                                },
                                "grantNumberValue": {
                                    "typeName": "grantNumberValue",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.GrantNumberValue }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{{ if .RelatedMaterialCitation }}{
                        "typeName": "relatedMaterial",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .RelatedMaterialCitation }}{{ if $index }},
                            {{ end }}{
                                "relatedMaterialCitation": {
                                    "typeName": "relatedMaterialCitation",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element }}"
                                }
                            }{{ end }}
                        ]
                    },{{ end }}{
                        "typeName": "author",
                        "multiple": true,
                        "typeClass": "compound",
                        "value": [
                            {{ range $index, $element := .Author }}{{ if $index }},
                            {{ end }}{
                                "authorName": {
                                    "typeName": "authorName",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.AuthorName }}"
                                }{{ if (ne $element.AuthorAffiliation "") }},
                                "authorAffiliation": {
                                    "typeName": "authorAffiliation",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.AuthorAffiliation }}"
                                }{{ end }}{{ if (ne $element.AuthorIdentifier "") }},
                                "authorIdentifierScheme": {
                                    "typeName": "authorIdentifierScheme",
                                    "multiple": false,
                                    "typeClass": "controlledVocabulary",
                                    "value": "ORCID"
                                },
                                "authorIdentifier": {
                                    "typeName": "authorIdentifier",
                                    "multiple": false,
                                    "typeClass": "primitive",
                                    "value": "{{ $element.AuthorIdentifier }}"
                                }{{ end }}
                            }{{ end }}
                        ]
                    }
                ]
            }
        }
    }
}
`))
