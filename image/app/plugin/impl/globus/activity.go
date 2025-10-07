// Author: Eryk Kulikowski @ KU Leuven (2025). Apache 2.0 License

package globus

import (
	"context"
	"fmt"
	"integration/app/config"
	"net/url"
	"strings"
)

func TaskActivityURL(taskId string) string {
	base := config.GetGlobusWebAppUrl()
	trimmed := strings.TrimSuffix(base, "/")
	escaped := url.PathEscape(taskId)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", trimmed, escaped)
}

func GetTaskStatus(ctx context.Context, token, taskId string) ([]byte, error) {
	escaped := url.PathEscape(taskId)
	endpoint := fmt.Sprintf("https://transfer.api.globusonline.org/v0.10/task/%s", escaped)
	return DoGlobusRequest(ctx, endpoint, "GET", token, nil)
}
