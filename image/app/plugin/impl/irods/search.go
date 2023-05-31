// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package irods

import (
	"context"
	"integration/app/logging"
	"integration/app/plugin/types"
)

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	zones, err := getZones(params.Token)
	if err != nil {
		logging.Logger.Println("getting zones failed: " + err.Error())
		return nil, nil
	}
	res := []types.SelectItem{}
	for _, z := range zones {
		res = append(res, types.SelectItem{
			Label: z.Zone,
			Value: z.Zone,
		})
	}
	return res, nil
}
