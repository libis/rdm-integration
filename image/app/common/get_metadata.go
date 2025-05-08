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
		logging.Logger.Println("metadata request read error", err)
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - bad request"))
		logging.Logger.Println("metadata request unmarshal error", err)
		return
	}

	user := core.GetUserFromHeader(r.Header)
	sessionId := core.GetSessionId(r.Header)
	metadata, err := getMetadata(r.Context(), req, user, sessionId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - matadata retrieval failed: " + err.Error()))
		logging.Logger.Println("metadata request get metadata error", err)
		return
	}

	b, err = json.Marshal(metadata)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		logging.Logger.Println("metadata response marshal error", err)
		return
	}
	logging.Logger.Println("metadata response", string(b))
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
	md := types.MetadataStruct{Author: []types.Author{{AuthorName: jsonEscape(fmt.Sprintf("%v, %v", userObj.Data.LastName, userObj.Data.FirstName))}}}

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
		to.Title = jsonEscape(from.Title)
	}
	altTitle := jsonEscapeSlice(from.AlternativeTitle)
	if len(altTitle) > 0 {
		to.AlternativeTitle = altTitle
	}
	altUrl := jsonEscapeSlice(from.AlternativeURL)
	if len(altUrl) > 0 {
		to.AlternativeURL = altUrl
	}
	otherIds := []types.OtherId{}
	for _, oi := range from.OtherId {
		if oi.OtherIdAgency != "" && oi.OtherIdValue != "" {
			otherIds = append(otherIds, types.OtherId{OtherIdAgency: jsonEscape(oi.OtherIdAgency), OtherIdValue: jsonEscape(oi.OtherIdValue)})
		}
	}
	if len(otherIds) > 0 {
		to.OtherId = otherIds
	}
	descriptions := jsonEscapeSlice(from.DsDescription)
	if len(descriptions) > 0 {
		to.DsDescription = descriptions
	}
	keywords := jsonEscapeSlice(from.Keyword)
	if len(keywords) > 0 {
		to.Keyword = keywords
	}
	contributors := jsonEscapeSlice(from.ContributorName)
	if len(contributors) > 0 {
		to.ContributorName = contributors
	}
	citations := jsonEscapeSlice(from.RelatedMaterialCitation)
	if len(citations) > 0 {
		to.RelatedMaterialCitation = citations
	}
	grantNumbers := []types.GrantNumber{}
	for _, gn := range from.GrantNumber {
		if gn.GrantNumberAgency != "" && gn.GrantNumberValue != "" {
			grantNumbers = append(grantNumbers, types.GrantNumber{GrantNumberAgency: jsonEscape(gn.GrantNumberAgency), GrantNumberValue: jsonEscape(gn.GrantNumberValue)})
		}
	}
	if len(grantNumbers) > 0 {
		to.GrantNumber = grantNumbers
	}
	authors := []types.Author{}
	for _, a := range from.Author {
		if a.AuthorName != "" {
			authors = append(authors, types.Author{AuthorName: jsonEscape(a.AuthorName), AuthorAffiliation: jsonEscape(a.AuthorAffiliation), AuthorIdentifier: jsonEscape(a.AuthorIdentifier)})
		}
	}
	if len(authors) > 0 {
		to.Author = authors
	}
	return to
}

func jsonEscapeSlice(in []string) []string {
	escaped := []string{}
	for _, v := range in {
		if v != "" {
			escaped = append(escaped, jsonEscape(v))
		}
	}
	return escaped
}

func jsonEscape(in string) string {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	return string(b[1 : len(b)-1])
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
	codemeta := map[string]interface{}{}
	err = json.Unmarshal(b, &codemeta)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	res := types.MetadataStruct{}
	res.Title, _ = codemeta["name"].(string)
	authors, _ := codemeta["author"].([]interface{})
	for _, a := range authors {
		a, _ := a.(map[string]interface{})
		givenName, _ := a["givenName"].(string)
		familyName, _ := a["familyName"].(string)
		affiliation, _ := a["affiliation"].(map[string]interface{})
		affiliationName, _ := affiliation["name"].(string)
		identifier, _ := a["id"].(string)
		if !strings.Contains(identifier, "orcid") {
			identifier = ""
		}
		if familyName != "" {
			res.Author = append(res.Author, types.Author{
				AuthorName:        fmt.Sprintf("%s, %s", familyName, givenName),
				AuthorAffiliation: affiliationName,
				AuthorIdentifier:  identifier,
			})
		}
	}
	contributors, _ := codemeta["contributor"].([]interface{})
	for _, a := range contributors {
		a, _ := a.(map[string]interface{})
		givenName, _ := a["givenName"].(string)
		familyName, _ := a["familyName"].(string)
		if familyName != "" {
			res.ContributorName = append(res.ContributorName, fmt.Sprintf("%s, %s", familyName, givenName))
		}
	}
	description, _ := codemeta["description"].(string)
	res.DsDescription = []string{description}
	keywords, _ := codemeta["keywords"].([]interface{})
	for _, k := range keywords {
		keyword, _ := k.(string)
		res.Keyword = append(res.Keyword, keyword)
	}
	funder, ok := codemeta["funder"].(map[string]interface{})
	funding, _ := codemeta["funding"].(string)
	if ok {
		funderName, _ := funder["name"].(string)
		res.GrantNumber = append(res.GrantNumber, types.GrantNumber{GrantNumberAgency: funderName, GrantNumberValue: funding})
	}
	return res, nil
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
	multiline := false
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
				} else if multiline {
					res.DsDescription[0] = res.DsDescription[0] + "\n" + joinAndUnescape([]string{line[len("  "):]})
				}
			}
		} else {
			s := strings.Split(line, ": ")
			switch s[0] {
			case "title":
				res.Title = joinAndUnescape(s[1:])
				keywords = false
				authors = false
				multiline = false
			case "abstract":
				description := joinAndUnescape(s[1:])
				if description == ">-" {
					multiline = true
					res.DsDescription = []string{""}
				} else {
					res.DsDescription = []string{description}
				}
				keywords = false
				authors = false
			case "keywords:":
				keywords = true
				authors = false
				multiline = false
			case "authors:":
				authors = true
				keywords = false
				multiline = false
			default:
				keywords = false
				authors = false
				multiline = false
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
	if strings.HasPrefix(res, "\"") && strings.HasSuffix(res, "\"") {
		res = res[1 : len(res)-1]
	}
	res = strings.ReplaceAll(res, "''", "'")
	res = strings.ReplaceAll(res, "\"\"", "\"")
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
