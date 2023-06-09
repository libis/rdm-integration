// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package dataverse

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type dvVersion string

var version dvVersion

var filesCleanup = "5.13"
var urlSigning = "5.14"
var directUpload = "https://github.com/IQSS/dataverse/pull/9003"       // will be replaced with verion when pull request is merged
var slashInPermissions = "https://github.com/IQSS/dataverse/pull/8995" // will be replaced with verion when pull request is merged
var nativeApiDelete = "5.14"

func init() {
	if config.GetConfig().DataverseServer != "" {
		Init()
	}
}

func Init() {
	version = getVersion()
	if version.GreaterOrEqual(filesCleanup) {
		logging.Logger.Printf("version %v >= %v: files cleanup feature is on", version, filesCleanup)
		filesCleanup = "true"
	}
	if version.GreaterOrEqual(urlSigning) {
		logging.Logger.Printf("version %v >= %v: url signing feature is on", version, urlSigning)
		urlSigning = "true"
	}
	if version.GreaterOrEqual(directUpload) {
		logging.Logger.Printf("version %v >= %v: direct upload feature is on", version, directUpload)
		directUpload = "true"
	}
	if version.GreaterOrEqual(slashInPermissions) {
		logging.Logger.Printf("version %v >= %v: slash in permissions feature is on", version, slashInPermissions)
		slashInPermissions = "true"
	}
	if version.GreaterOrEqual(nativeApiDelete) {
		logging.Logger.Printf("version %v >= %v: native API delete feature is on", version, nativeApiDelete)
		nativeApiDelete = "true"
	}
}

func getVersion() dvVersion {
	ctx, cancel := context.WithTimeout(context.Background(), dvContextDuration)
	defer cancel()
	url := fmt.Sprintf("%s/api/v1/info/version", config.GetConfig().DataverseServer)
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logging.Logger.Println("error when getting version:", err)
		logging.Logger.Println("using default 5.13 version")
		return "5.13"
	}
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		logging.Logger.Println("error when getting version:", err)
		logging.Logger.Println("using default 5.13 version")
		return "5.13"
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	res := VersionResponse{}
	if r.StatusCode != 200 {
		logging.Logger.Println("error when getting version:", res.Message)
	}
	json.Unmarshal(b, &res)
	logging.Logger.Println("Dataverse version:", res.Data.Version)
	ver := res.Data.Version
	if ver == "" {
		logging.Logger.Println("using default 5.13 version")
		ver = "5.13"
	}
	return dvVersion(ver)
}

func (v1 dvVersion) GreaterOrEqual(v2 string) bool {
	split1 := strings.Split(string(v1), ".")
	split2 := strings.Split(v2, ".")
	l := len(split1)
	if len(split2) < l {
		l = len(split2)
	}
	for i := 0; i < l; i++ {
		n1, _ := strconv.Atoi(split1[i])
		n2, err := strconv.Atoi(split2[i])
		if err != nil || n1 < n2 {
			return false
		}
		if n1 > n2 {
			return true
		}
	}
	return len(v1) >= len(v2)
}
