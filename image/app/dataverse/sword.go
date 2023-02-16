// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"fmt"
	"integration/app/config"
	"io"
	"net/http"
)

func swordDelete(ctx context.Context, token, user string, id int64) error {
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", config.GetConfig().DataverseServer, id)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	if addTokenToHeader {
		request.SetBasicAuth(token, "")
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 202 && r.StatusCode != 204 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("deleting file %d failed: %d - %s", id, r.StatusCode, string(b))
	}
	return nil
}
