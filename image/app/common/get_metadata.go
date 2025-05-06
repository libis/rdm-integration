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
	"integration/app/logging"
	"integration/app/plugin"
	dv "integration/app/plugin/impl/dataverse"
	"integration/app/plugin/types"
	"integration/app/tree"
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
	sessionId := core.GetSessionId(r.Header)
	metadata, err := getMetadata(r.Context(), req, user, sessionId)
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

func getMetadata(ctx context.Context, getMetadataRequest types.GetMetadataRequest, user, sessionId string) (types.Metadata, error) {
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

	nodemap := map[string]tree.Node{}
	for _, v := range getMetadataRequest.CompareResult.Data {
		nodemap[v.Id] = v
	}
	p := plugin.GetPlugin(getMetadataRequest.PluginId)

	//TODO
	// citation.cff --> (2) codemeta.json --> (3) ro-crate.json --> (4) inherente system metadata (via de source API)
	/*
		md := MetadataStruct{
			Title:                   "some title",
			AlternativeTitle:        []string{"title1", "title2"},
			AlternativeURL:          []string{"url1", "url2"},
			OtherId:                 []OtherId{{OtherIdAgency: "cia", OtherIdValue: "007"}, {OtherIdAgency: "mi6", OtherIdValue: "007"}},
			DsDescription:           []string{"text", "tekst"},
			Keyword:                 []string{"key", "word"},
			ContributorName:         []string{"jos", "jef"},
			RelatedMaterialCitation: []string{"jos did it", "jef did it"},
			GrantNumber:             []GrantNumber{{GrantNumberValue: "007", GrantNumberAgency: "mi6"}},
			Author:                  []Author{{AuthorName: "bond, james bond"}},
		}
	*/

	streamParams := types.StreamParams{}
	streamParams.PluginId = getMetadataRequest.PluginId
	streamParams.RepoName = getMetadataRequest.RepoName
	streamParams.Url = getMetadataRequest.Url
	streamParams.Option = getMetadataRequest.Option
	streamParams.User = user
	streamParams.Token = core.GetTokenFromCache(ctx, getMetadataRequest.Token, sessionId, getMetadataRequest.PluginId)
	streamParams.DVToken = getMetadataRequest.DVToken
	streamParams.SessionId = sessionId

	citationCff, ok := nodemap["CITATION.cff"]
	if ok {
		moreMd, err := getMdFromCitatinCff(ctx, citationCff, p, streamParams)
		if err != nil {
			return nil, err
		}
		md = mergeMetadata(moreMd, md)
	}

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

func mergeMetadata(from, to types.MetadataStruct) types.MetadataStruct {
	if from.Title != "" {
		to.Title = from.Title
	}
	if len(from.AlternativeTitle) > 0 {
		to.AlternativeTitle = append(to.AlternativeTitle, from.AlternativeTitle...)
	}
	if len(from.AlternativeURL) > 0 {
		to.AlternativeURL = append(to.AlternativeURL, from.AlternativeURL...)
	}
	if len(from.OtherId) > 0 {
		to.OtherId = append(to.OtherId, from.OtherId...)
	}
	if len(from.DsDescription) > 0 {
		to.DsDescription = append(to.DsDescription, from.DsDescription...)
	}
	if len(from.Keyword) > 0 {
		to.Keyword = append(to.Keyword, from.Keyword...)
	}
	if len(from.ContributorName) > 0 {
		to.ContributorName = append(to.ContributorName, from.ContributorName...)
	}
	if len(from.RelatedMaterialCitation) > 0 {
		to.RelatedMaterialCitation = append(to.RelatedMaterialCitation, from.RelatedMaterialCitation...)
	}
	if len(from.GrantNumber) > 0 {
		to.GrantNumber = append(to.GrantNumber, from.GrantNumber...)
	}
	if len(from.Author) > 0 {
		to.Author = append(to.Author, from.Author...)
	}
	return to
}

func getMdFromCitatinCff(ctx context.Context, node tree.Node, p plugin.Plugin, params types.StreamParams) (types.MetadataStruct, error) {
    b, err := getFileFromRepo(ctx, node, p, params)
    if err != nil {
        return types.MetadataStruct{}, err
    }
    logging.Logger.Println(string(b))
	return types.MetadataStruct{}, nil
}

func getFileFromRepo(ctx context.Context, node tree.Node, p plugin.Plugin, params types.StreamParams) ([]byte, error) {
    node.Action = tree.Copy
	s, err := p.Streams(ctx, map[string]tree.Node{node.Id: node}, params)
	if err != nil {
		return nil, err
	}
	if s.Cleanup != nil {
		defer s.Cleanup()
	}
	stream := s.Streams[node.Id]
	if stream.Close != nil {
		defer stream.Close()
	}
	if stream.Open != nil {
		r, err := stream.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return b, nil
	} else {
        return nil, fmt.Errorf("getting %v from repo failed", node.Id)
    }
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
