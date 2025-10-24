// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"flag"
	"fmt"
	"integration/app/config"
	"integration/app/dataverse"
	"integration/app/destination"
	"integration/app/frontend"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/testutil"
	"integration/app/workers/spinner"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	DataverseServer     string
	DataverseServerName string
	RootDataverseId     string
	DefaultHash         string = "MD5"
	MyDataRoleIds       string = "1,6,7"
	MaxFileSize         string = "21474836480"
)

var (
	serverUrl   = flag.String("server", DataverseServer, "URL to the Dataverse server")
	serverName  = flag.String("servername", DataverseServerName, "Dataverse server display name")
	dvID        = flag.String("dvID", RootDataverseId, "Root Dataverse ID")
	hashAlg     = flag.String("hash", DefaultHash, "Default hashing algorithm in Dataverse: MD5, SHA-1")
	roleIDs     = flag.String("roleIDs", MyDataRoleIds, "My data query role IDs: comma separated ints")
	maxFileSize = flag.String("maxFileSize", MaxFileSize, "Maximum file size in bytes for upload.")
)

func main() {
	destination.SetDataverseAsDestination()
	logging.Logger.Println("execute with -h to see the list of possible arguments")
	flag.Parse()
	DataverseServer = *serverUrl
	DataverseServerName = *serverName
	if DataverseServerName == "" {
		DataverseServerName = DataverseServer
	}
	RootDataverseId = *dvID
	DefaultHash = *hashAlg
	MyDataRoleIds = *roleIDs
	roles := []int{}
	tmp := strings.Split(MyDataRoleIds, ",")
	for i := 0; i < len(tmp); i++ {
		id, _ := strconv.Atoi(strings.TrimSpace(tmp[i]))
		roles = append(roles, id)
	}
	MaxFileSize = *maxFileSize
	mfs, _ := strconv.Atoi(MaxFileSize)
	config.SetConfig(DataverseServer, RootDataverseId, DefaultHash, roles, true, int64(mfs))
	dataverse.Init()
	frontend.Config.DataverseHeader = DataverseServerName
	frontend.Config.Plugins = append([]config.RepoPlugin{{
		Id:                        "local",
		Name:                      "Local filesystem",
		Plugin:                    "local",
		PluginName:                "Local filesystem",
		SourceUrlFieldName:        "Directory",
		SourceUrlFieldPlaceholder: "Path to a directory on your filesystem",
	}}, frontend.Config.Plugins...)
	go server.Start()
	fr := testutil.NewFakeRedis()
	config.SetRedis(fr)
	openBrowser("http://localhost:7788/")

	ticker := time.NewTicker(5 * time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fr.CleanupExpired()
			}
		}
	}()

	spinner.SpinWorkers(1, "ALL")
	ticker.Stop()
	done <- true
}

func openBrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		logging.Logger.Fatal(err)
	}
}
