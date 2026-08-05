// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"integration/app/config"
	"integration/app/logging"
)

// GetMaxFileSize returns the effective maximum file size for uploads: the
// smaller of the locally configured limit and the limit configured on the
// Dataverse server (:MaxFileUploadSizeInBytes). Querying the server keeps the
// dashboard's pre-check in sync with the server so that files over the server
// limit are visibly rejected at compare time instead of being silently
// refused during registration. Zero means no limit.
func GetMaxFileSize(ctx context.Context) int64 {
	return effectiveMaxFileSize(config.GetMaxFileSize(), serverMaxFileSize(ctx))
}

func effectiveMaxFileSize(configured, server int64) int64 {
	if configured <= 0 {
		return server
	}
	if server > 0 && server < configured {
		return server
	}
	return configured
}

func serverMaxFileSize(ctx context.Context) int64 {
	// Public endpoint, available on newer Dataverse versions.
	if size, ok := querySetting(ctx, config.GetConfig().DataverseServer+"/api/v1/info/settings/:MaxFileUploadSizeInBytes"); ok {
		return size
	}
	// Older versions do not expose the setting publicly; fall back to the
	// admin API using the unblock key when one is configured. The admin API
	// is reachable here because the backend talks to Dataverse over the
	// internal network.
	if config.UnblockKey != "" {
		u := config.GetConfig().DataverseServer + "/api/v1/admin/settings/:MaxFileUploadSizeInBytes?unblock-key=" + url.QueryEscape(config.UnblockKey)
		if size, ok := querySetting(ctx, u); ok {
			return size
		}
	}
	return 0
}

func querySetting(ctx context.Context, u string) (int64, bool) {
	request, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, false
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		logging.Logger.Println("querying server max file size failed:", err)
		return 0, false
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return 0, false
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, false
	}
	res := struct {
		Status string `json:"status"`
		Data   struct {
			Message string `json:"message"`
		} `json:"data"`
	}{}
	if err := json.Unmarshal(b, &res); err != nil || res.Status != "OK" {
		return 0, false
	}
	return parseSetting(res.Data.Message)
}

func parseSetting(value string) (int64, bool) {
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size < 0 {
		if err != nil {
			logging.Logger.Println(fmt.Sprintf("server max file size setting is not a valid number: %q", value))
		}
		return 0, false
	}
	return size, true
}
