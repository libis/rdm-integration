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

func main() {
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
	return redis.NewStatusCmd(ctx, "PONG")
}

func (f *fakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	f.Lock()
	defer f.Unlock()
	v := f.values[key]
	if f.expirations[key].After(time.Now()) {
		v = ""
	}
	return redis.NewStringCmd(ctx, v)
}

func (f *fakeRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	f.Lock()
	defer f.Unlock()
	f.values[key] = fmt.Sprintf("%v", value)
	delete(f.expirations, key)
	return redis.NewStatusCmd(ctx, "OK")
}

func (f *fakeRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.Lock()
	defer f.Unlock()
	f.values[key] = fmt.Sprintf("%v", value)
	f.expirations[key] = time.Now().Add(expiration)
	return redis.NewBoolCmd(ctx, true)
}

func (f *fakeRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	f.Lock()
	defer f.Unlock()
	for _, key := range keys {
		delete(f.values, key)
	}
	return redis.NewIntCmd(ctx, len(keys))
}

func (f *fakeRedis) LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	f.Lock()
	defer f.Unlock()
	newValues := []string{}
	for _, v := range values {
		newValues = append(newValues, fmt.Sprintf("%v", v))
	}
	f.valueSlices[key] = append(newValues, f.valueSlices[key]...)
	return redis.NewIntCmd(ctx, len(key))
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
	return redis.NewStringCmd(ctx, v)
}
