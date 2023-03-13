// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sync"
	"time"
)

type Job struct {
	DataverseKey      string
	User              string
	SessionId         string
	PersistentId      string
	WritableNodes     map[string]tree.Node
	Plugin            string
	Streams           map[string]map[string]interface{}
	StreamParams      types.StreamParams
	ErrCnt            int
	Deadline          time.Time
	SendEmailOnSucces bool
}

var Stop = make(chan struct{})
var Wait = sync.WaitGroup{}

var redisCtxDuration = 5 * time.Second

func IsLocked(ctx context.Context, persistentId string) bool {
	l := config.GetRedis().Get(ctx, "lock: "+persistentId)
	return l.Val() != ""
}

func lock(persistentId string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), redisCtxDuration)
	defer cancel()
	ok := config.GetRedis().SetNX(ctx, "lock: "+persistentId, true, config.LockMaxDuration)
	return ok.Val()
}

func unlock(persistentId string) {
	ctx, cancel := context.WithTimeout(context.Background(), redisCtxDuration)
	defer cancel()
	config.GetRedis().Del(ctx, "lock: "+persistentId)
}

func AddJob(ctx context.Context, job Job) error {
	if len(job.WritableNodes) == 0 {
		return nil
	}
	err := addJob(ctx, job, true)
	if err == nil {
		logging.Logger.Println("job added for " + job.PersistentId)
	}
	return err
}

func addJob(ctx context.Context, job Job, requireLock bool) error {
	if len(job.WritableNodes) == 0 {
		return nil
	}
	if requireLock && !lock(job.PersistentId) {
		return fmt.Errorf("Job for this dataverse is already in progress")
	}
	if requireLock {
		job.Deadline = time.Now().Add(config.LockMaxDuration)
	}
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	cmd := config.GetRedis().LPush(ctx, "jobs", string(b))
	return cmd.Err()
}

func popJob() (Job, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), redisCtxDuration)
	defer cancel()
	cmd := config.GetRedis().RPop(ctx, "jobs")
	err := cmd.Err()
	if err != nil {
		return Job{}, false
	}
	v := cmd.Val()
	job := Job{}
	err = json.Unmarshal([]byte(v), &job)
	if err != nil {
		logging.Logger.Println("failed to unmarshall a job:", err)
		return job, false
	}
	return job, true
}

func ProcessJobs() {
	defer Wait.Done()
	defer logging.Logger.Println("worker exited grecefully")
	for {
		select {
		case <-Stop:
			return
		case <-time.After(1 * time.Second):
		}
		job, ok := popJob()
		if ok {
			persistentId := job.PersistentId
			logging.Logger.Printf("%v: job started\n", persistentId)
			job, err := doWork(job)
			if err != nil {
				job.ErrCnt = job.ErrCnt + 1
				if job.ErrCnt == 3 {
					logging.Logger.Println("job failed and will not be retried:", persistentId, err)
					sendJobFailedMail(err, job)
				} else {
					logging.Logger.Println("job failed, but will retry:", persistentId, err)
				}
			}
			if len(job.WritableNodes) > 0 && job.ErrCnt < 3 {
				ctx, cancel := context.WithTimeout(context.Background(), redisCtxDuration)
				err = addJob(ctx, job, false)
				cancel()
				if err != nil {
					logging.Logger.Println("re-adding job failed (no retry):", persistentId, err)
					unlock(persistentId)
				}
			} else {
				unlock(persistentId)
				logging.Logger.Printf("%v: job ended\n", persistentId)
			}
		}
	}
}
