// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

func swordDelete(ctx context.Context, token, user string, id int64) error {
	shortContext, cancel := context.WithTimeout(ctx, deleteAndCleanupCtxDuration)
	defer cancel()
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", config.DataverseServer, id)
	url, addTokenToHeader, err := signUrl(ctx, url, token, user)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(shortContext, "DELETE", url, nil)
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
