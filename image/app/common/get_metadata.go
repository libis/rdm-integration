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
	"strings"
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
	p := plugin.GetPlugin(getMetadataRequest.Plugin)

	streamParams := types.StreamParams{}
	streamParams.PluginId = getMetadataRequest.PluginId
	streamParams.RepoName = getMetadataRequest.RepoName
	streamParams.Url = getMetadataRequest.Url
	streamParams.Option = getMetadataRequest.Option
	streamParams.User = user
	streamParams.Token = core.GetTokenFromCache(ctx, getMetadataRequest.Token, sessionId, getMetadataRequest.PluginId)
	streamParams.DVToken = getMetadataRequest.DVToken
	streamParams.SessionId = sessionId

	if p.Metadata != nil {
		//TODO
		moreMd, err := p.Metadata(ctx, streamParams)
		if err != nil {
			return nil, err
		}
		md = mergeMetadata(moreMd, md)
	}

	roCrate, ok := nodemap["ro-crate.json"]
	if ok {
		moreMd, err := getMdFromROCrate(ctx, roCrate, p, streamParams)
		if err != nil {
			return nil, err
		}
		md = mergeMetadata(moreMd, md)
	}

	codemeta, ok := nodemap["codemeta.json"]
	if ok {
		moreMd, err := getMdFromCodemeta(ctx, codemeta, p, streamParams)
		if err != nil {
			return nil, err
		}
		md = mergeMetadata(moreMd, md)
	}

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

func getMdFromROCrate(ctx context.Context, node tree.Node, p plugin.Plugin, params types.StreamParams) (types.MetadataStruct, error) {
	b, err := getFileFromRepo(ctx, node, p, params)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	//TODO
	logging.Logger.Println(string(b))
	return types.MetadataStruct{}, nil
}

func getMdFromCodemeta(ctx context.Context, node tree.Node, p plugin.Plugin, params types.StreamParams) (types.MetadataStruct, error) {
	b, err := getFileFromRepo(ctx, node, p, params)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	//TODO
	logging.Logger.Println(string(b))
	return types.MetadataStruct{}, nil
}

type author struct {
	GivenNames  string
	FamilyNames string
	Affiliation string
	Orcid       string
}

func getMdFromCitatinCff(ctx context.Context, node tree.Node, p plugin.Plugin, params types.StreamParams) (types.MetadataStruct, error) {
	res := types.MetadataStruct{}
	b, err := getFileFromRepo(ctx, node, p, params)
	if err != nil {
		return res, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	keywords := false
	authors := false
	authorIndex := -1
	foundAuthors := []author{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  ") {
			if strings.HasPrefix(line, "  - ") {
				if authors {
					authorIndex = authorIndex + 1
					foundAuthors = append(foundAuthors, author{})
					s := strings.Split(line, ": ")
					if len(s) > 0 {
						foundAuthors[authorIndex] = setAuthorField(s[0][len("  - "):], joinAndUnescape(s[1:]), foundAuthors[authorIndex])
					}
				} else if keywords {
					res.Keyword = append(res.Keyword, joinAndUnescape([]string{line[len("  - "):]}))
				}
			} else {
				if authors {
					s := strings.Split(line, ": ")
					if len(s) > 0 {
						foundAuthors[authorIndex] = setAuthorField(s[0][len("    "):], joinAndUnescape(s[1:]), foundAuthors[authorIndex])
					}
				}
			}
		} else {
			s := strings.Split(line, ": ")
			switch s[0] {
			case "title":
				res.Title = joinAndUnescape(s[1:])
				keywords = false
				authors = false
			case "abstract":
				res.DsDescription = []string{joinAndUnescape(s[1:])}
				keywords = false
				authors = false
			case "keywords:":
				keywords = true
				authors = false
			case "authors:":
				authors = true
				keywords = false
			default:
				keywords = false
				authors = false
			}
		}
	}
	for _, a := range foundAuthors {
		if a.FamilyNames != "" {
			res.Author = append(res.Author, types.Author{
				AuthorName:        fmt.Sprintf("%s, %s", a.FamilyNames, a.GivenNames),
				AuthorAffiliation: a.Affiliation,
				AuthorIdentifier:  a.Orcid,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return res, err
	}
	return res, nil
}

func setAuthorField(field, value string, in author) author {
	switch field {
	case "given-names":
		in.GivenNames = value
	case "family-names":
		in.FamilyNames = value
	case "affiliation":
		in.Affiliation = value
	case "orcid":
		in.Orcid = value
	}
	return in
}

func joinAndUnescape(s []string) string {
	res := strings.Join(s, ": ")
	if strings.HasPrefix(res, "'") && strings.HasSuffix(res, "'") {
		res = res[1 : len(res)-1]
	}
	res = strings.ReplaceAll(res, "''", "'")
	return res
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
