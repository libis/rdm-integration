// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// FakeRedis is a simple in-memory Redis mock for testing
type FakeRedis struct {
	sync.Mutex
	values      map[string]string
	expirations map[string]time.Time
	valueSlices map[string][]string
}

// NewFakeRedis creates a new fake Redis instance
func NewFakeRedis() *FakeRedis {
	return &FakeRedis{
		values:      make(map[string]string),
		expirations: make(map[string]time.Time),
		valueSlices: make(map[string][]string),
	}
}

func (f *FakeRedis) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("PONG")
	return cmd
}

func (f *FakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
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

func (f *FakeRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
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

// SetNX - set if Not eXists
func (f *FakeRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
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

func (f *FakeRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
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

func (f *FakeRedis) LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
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

func (f *FakeRedis) RPop(ctx context.Context, key string) *redis.StringCmd {
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

// CleanupExpired removes expired keys (useful for testing)
func (f *FakeRedis) CleanupExpired() {
	f.Lock()
	defer f.Unlock()
	for k, v := range f.expirations {
		if v.Before(time.Now()) {
			delete(f.expirations, k)
			delete(f.values, k)
		}
	}
}

// Reset clears all data (useful for test cleanup)
func (f *FakeRedis) Reset() {
	f.Lock()
	defer f.Unlock()
	f.values = make(map[string]string)
	f.expirations = make(map[string]time.Time)
	f.valueSlices = make(map[string][]string)
}
