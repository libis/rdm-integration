// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"context"
	"flag"
	"fmt"
	"integration/app/frontend"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/utils"
	"integration/app/workers/spinner"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v9"
)

var (
	DataverseServer     string
	DataverseServerName string
	RootDataverseId     string
	DefaultHash         string = "MD5"
	MyDataRoleIds       string = "6,7"
)

var (
	serverUrl  = flag.String("server", DataverseServer, "URL to the Dataverse server")
	serverName = flag.String("servername", DataverseServerName, "Dataverse server display name")
	dvID       = flag.String("dvID", RootDataverseId, "Root Dataverse ID")
	hashAlg    = flag.String("hash", DefaultHash, "Default hashing algorithm in Dataverse: MD5, SHA-1")
	roleIDs    = flag.String("roleIDs", MyDataRoleIds, "My data query role IDs: comma separated ints")
)

func main() {
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
	utils.SetConfig(DataverseServer, RootDataverseId, DefaultHash, roles, true)
	frontend.Config.DataverseHeader = DataverseServerName
	frontend.Config.Plugins = append([]frontend.RepoPlugin{{
		Id:                        "local",
		Name:                      "Local filesystem",
		SourceUrlFieldName:        "Directory",
		SourceUrlFieldPlaceholder: "Path to a directory on your filesystem",
	}}, frontend.Config.Plugins...)
	go server.Start()
	utils.SetRedis(newFakeRedis())
	openbrowser("http://localhost:7788/")
	spinner.SpinWorkers(1)
}

func openbrowser(url string) {
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

type fakeRedis struct {
	sync.Mutex
	values      map[string]string
	expirations map[string]time.Time
	valueSlices map[string][]string
}

func newFakeRedis() *fakeRedis {
	f := fakeRedis{
		values:      make(map[string]string),
		expirations: make(map[string]time.Time),
		valueSlices: make(map[string][]string),
	}
	return &f
}

func (f *fakeRedis) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("PONG")
	return cmd
}

func (f *fakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	f.Lock()
	defer f.Unlock()
	v := f.values[key]
	exp, ok := f.expirations[key]
	if ok && exp.Before(time.Now()) {
		v = ""
		delete(f.values, key)
		delete(f.expirations, key)
	}
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal(v)
	return cmd
}

func (f *fakeRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	f.Lock()
	defer f.Unlock()
	f.values[key] = fmt.Sprintf("%v", value)
	if expiration > 0 {
		f.expirations[key] = time.Now().Add(expiration)
	} else {
		delete(f.expirations, key)
	}
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("OK")
	return cmd
}

// set if Not eXists
func (f *fakeRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.Lock()
	defer f.Unlock()
	cmd := redis.NewBoolCmd(ctx)
	_, ok := f.values[key]
	if ok {
		exp, ok2 := f.expirations[key]
		if !ok2 || exp.After(time.Now()) {
			cmd.SetVal(false)
			return cmd
		}
	}
	f.values[key] = fmt.Sprintf("%v", value)
	if expiration > 0 {
		f.expirations[key] = time.Now().Add(expiration)
	} else {
		delete(f.expirations, key)
	}
	cmd.SetVal(true)
	return cmd
}

func (f *fakeRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	f.Lock()
	defer f.Unlock()
	for _, key := range keys {
		delete(f.values, key)
		delete(f.expirations, key)
	}
	cmd := redis.NewIntCmd(ctx)
	cmd.SetVal(int64(len(keys)))
	return cmd
}

func (f *fakeRedis) LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	f.Lock()
	defer f.Unlock()
	newValues := []string{}
	for _, v := range values {
		newValues = append(newValues, fmt.Sprintf("%v", v))
	}
	f.valueSlices[key] = append(newValues, f.valueSlices[key]...)
	cmd := redis.NewIntCmd(ctx)
	cmd.SetVal(int64(len(key)))
	return cmd
}

func (f *fakeRedis) RPop(ctx context.Context, key string) *redis.StringCmd {
	f.Lock()
	defer f.Unlock()
	l := len(f.valueSlices[key])
	if l == 0 {
		res := redis.NewStringCmd(ctx)
		res.SetErr(fmt.Errorf("queue is empty"))
		return res
	}
	v := f.valueSlices[key][l-1]
	f.valueSlices[key] = f.valueSlices[key][:l-1]
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal(v)
	return cmd
}
