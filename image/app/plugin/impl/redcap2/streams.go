// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"bytes"
	"context"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"strings"
)

func Streams(ctx context.Context, in map[string]tree.Node, streamParams types.StreamParams) (types.StreamsType, error) {
	if streamParams.Url == "" || streamParams.Token == "" {
		return types.StreamsType{}, fmt.Errorf("streams: missing parameters: expected url, token")
	}

	opts, err := parsePluginOptions(streamParams.PluginOptions)
	if err != nil {
		return types.StreamsType{}, err
	}

	reportID := opts.ReportID
	if reportID == "" {
		reportID = strings.TrimSpace(streamParams.Option)
	}
	if reportID == "" && opts.ExportMode != "records" {
		return types.StreamsType{}, fmt.Errorf("streams: missing report id")
	}
	opts.ReportID = reportID

	bundle, err := cachedBuildExportBundle(ctx, streamParams.Url, streamParams.Token, opts, reportID)
	if err != nil {
		return types.StreamsType{}, err
	}

	res := make(map[string]types.Stream, len(in))
	for key, node := range in {
		path := strings.TrimSpace(node.Attributes.URL)
		if path == "" {
			path = strings.TrimSpace(node.Id)
		}
		payload, ok := bundle.Files[path]
		if !ok {
			return types.StreamsType{}, fmt.Errorf("streams: generated file not found: %s", path)
		}
		// Copy payload to ensure stream readers are isolated from map aliasing.
		data := append([]byte(nil), payload...)
		res[key] = byteStream(data)
	}

	return types.StreamsType{
		Streams: res,
		Cleanup: nil,
	}, nil
}

func byteStream(data []byte) types.Stream {
	return types.Stream{
		Open: func() (io.Reader, error) {
			return bytes.NewReader(data), nil
		},
		Close: func() error {
			return nil
		},
	}
}
