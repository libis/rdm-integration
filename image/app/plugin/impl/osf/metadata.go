package osf

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
)

type ContributorsRes struct {
	Data []Data `json:"data"`
}

func Metadata(ctx context.Context, streamParams types.StreamParams) (types.MetadataStruct, error) {
	token := streamParams.Token
	if token == "" {
		return types.MetadataStruct{}, fmt.Errorf("metadata: missing parameters: expected token")
	}

	url := fmt.Sprintf("%s/v2/nodes/%s/", streamParams.Url, streamParams.RepoName)
	data, err := getData(ctx, url, token)
	if err != nil {
		return types.MetadataStruct{}, err
	}

	b, err := query(ctx, data.Relationships.Contributors.LinksWithHref.Related.Href, token)
	if err != nil {
		return types.MetadataStruct{}, err
	}
	res := ContributorsRes{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return types.MetadataStruct{}, fmt.Errorf("%s", string(b))
	}

	authors := []types.Author{}
	for _, v := range res.Data {
		author := types.Author{AuthorName: fmt.Sprintf("%s, %s", v.Embeds.Users.Data.Attributes.FamilyName, v.Embeds.Users.Data.Attributes.GivenName)}
		if v.Embeds.Users.Data.Attributes.Social.Orcid != "" {
			author.AuthorIdentifier = v.Embeds.Users.Data.Attributes.Social.Orcid
		}
		for _, employment := range v.Embeds.Users.Data.Attributes.Employment {
			author.AuthorAffiliation = employment.Institution
			break
		}
		authors = append(authors, author)
	}

	return types.MetadataStruct{
		Title:          data.Attributes.Title,
		AlternativeURL: []string{fmt.Sprintf("https://osf.io/%v/", data.Id)},
		DsDescription:  []string{data.Attributes.Description},
		Keyword:        data.Attributes.Tags,
		Author:         authors,
	}, nil
}
