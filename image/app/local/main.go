package main

import (
	"context"
	"fmt"
	"integration/app/logging"
	"integration/app/server"
	"integration/app/utils"
	"integration/app/workers/spinner"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/go-redis/redis/v9"
)

var DataverseServer string
var RootDataverseId string
var DefaultHash string

func main() {
	utils.SetConfig(DataverseServer, RootDataverseId, DefaultHash)
	logging.Logger.Printf("DataverseServer=%v, RootDataverseId=%v, DefaultHash=%v", DataverseServer, RootDataverseId, DefaultHash)
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
	if ok && exp.After(time.Now()) {
		v = ""
	}
	cmd := redis.NewStringCmd(ctx)
	cmd.SetVal(v)
	return cmd
}

func (f *fakeRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	f.Lock()
	defer f.Unlock()
	f.values[key] = fmt.Sprintf("%v", value)
	delete(f.expirations, key)
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("OK")
	return cmd
}

func (f *fakeRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.Lock()
	defer f.Unlock()
	f.values[key] = fmt.Sprintf("%v", value)
	f.expirations[key] = time.Now().Add(expiration)
	cmd := redis.NewBoolCmd(ctx)
	cmd.SetVal(true)
	return cmd
}

func (f *fakeRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	f.Lock()
	defer f.Unlock()
	for _, key := range keys {
		delete(f.values, key)
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
