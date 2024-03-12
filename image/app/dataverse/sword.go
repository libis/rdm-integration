// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"archive/zip"
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/core"
	"io"
	"net/http"
	"sync"
)

func swordDelete(ctx context.Context, token, _ string, id int64) error {
	url := fmt.Sprintf("%s/dvn/api/data-deposit/v1.1/swordv2/edit-media/file/%d", config.GetConfig().DataverseServer, id)
	request, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	request.SetBasicAuth(token, "")
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

func uploadViaSword(ctx context.Context, _ int64, id, token, _, persistentId string, wg *sync.WaitGroup, async_err *core.ErrorHolder) (io.WriteCloser, error) {
	url := config.GetConfig().DataverseServer + "/dvn/api/data-deposit/v1.1/swordv2/edit-media/study/" + persistentId
	pr, pw := io.Pipe()
	zipWriter := zip.NewWriter(pw)
	writer, _ := zipWriter.Create(id)
	request, _ := http.NewRequestWithContext(ctx, "POST", url, pr)
	request.Header.Add("Content-Type", "application/zip")
	request.Header.Add("Content-Disposition", "attachment;filename=example.zip")
	request.Header.Add("Packaging", "http://purl.org/net/sword/package/SimpleZip")
	request.SetBasicAuth(token, "")

	wg.Add(1)
	go func(req http.Request) {
		defer wg.Done()
		defer pr.Close()
		resp, err := http.DefaultClient.Do(request)
		if err != nil {
			if async_err != nil {
				async_err.Err = err
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 && async_err != nil {
			b, _ := io.ReadAll(resp.Body)
			async_err.Err = fmt.Errorf("writing file in %s failed: %d - %s", persistentId, resp.StatusCode, string(b))
		}
	}(*request)

	return core.NewWritterCloser(writer, zipWriter, pw), nil
}
