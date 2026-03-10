// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"fmt"
	"integration/app/plugin/types"
	"strings"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("options: missing parameters: expected url, token")
	}

	opts, err := parsePluginOptions(params.PluginOptions)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(opts.Request, "variables") {
		if opts.ExportMode == "records" {
			return listVariablesFromMetadata(ctx, params.Url, params.Token)
		}
		reportID := opts.ReportID
		if reportID == "" {
			reportID = strings.TrimSpace(params.Option)
		}
		if reportID == "" {
			return nil, fmt.Errorf("options: missing report id for variable lookup")
		}
		return listVariablesFromReport(ctx, params.Url, params.Token, reportID, opts)
	}

	// The REDCap API does not provide a standard endpoint to list reports.
	// Report IDs are entered manually by the user on the export settings page.
	return []types.SelectItem{}, nil
}
