package osf

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
)

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

	return types.MetadataStruct{
		Title:         data.Attributes.Title,
		DsDescription: []string{data.Attributes.Description},
		Keyword:       data.Attributes.Tags,
	}, nil
}
