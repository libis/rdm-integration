// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sync"
	"time"
)

type Job struct {
	DataverseKey  string
	PersistentId  string
	WritableNodes map[string]tree.Node
	StreamType    string
	Streams       map[string]map[string]interface{}
	StreamParams  types.StreamParams
	ErrCnt        int
}

var Stop = make(chan struct{})
var Wait = sync.WaitGroup{}

var lockMaxDuration = time.Hour * 24

func IsLocked(persistentId string) bool {
	l := GetRedis().Get(context.Background(), "lock: "+persistentId)
	return l.Val() != ""
}

func lock(persistentId string) bool {
	ok := GetRedis().SetNX(context.Background(), "lock: "+persistentId, true, lockMaxDuration)
	return ok.Val()
}

func unlock(persistentId string) {
	GetRedis().Del(context.Background(), "lock: "+persistentId)
}

func AddJob(job Job) error {
	if len(job.WritableNodes) == 0 {
		return nil
	}
	err := addJob(job, true)
	if err == nil {
		logging.Logger.Println("job added for " + job.PersistentId)
	}
	return err
}

func addJob(job Job, requireLock bool) error {
	if len(job.WritableNodes) == 0 {
		return nil
	}
	if requireLock && !lock(job.PersistentId) {
		return fmt.Errorf("Job for this dataverse is already in progress")
	}
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	cmd := GetRedis().LPush(context.Background(), "jobs", string(b))
	return cmd.Err()
}

func popJob() (Job, bool) {
	cmd := GetRedis().RPop(context.Background(), "jobs")
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
				} else {
					logging.Logger.Println("job failed, but will retry:", persistentId, err)
				}
			}
			if len(job.WritableNodes) > 0 && job.ErrCnt < 3 {
				err = addJob(job, false)
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
