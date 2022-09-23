package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/tree"
	"sync"
	"time"
)

type Job struct {
	DataverseKey  string
	Doi           string
	WritableNodes map[string]tree.Node
	StreamType    string
	Streams       map[string]map[string]interface{}
	StreamParams  map[string]string
}

var Stop = make(chan struct{})
var Wait = sync.WaitGroup{}

var lockMaxDuration = time.Hour * 24

func lock(doi string) bool {
	ok := rdb.SetNX(context.Background(), "lock: "+doi, true, lockMaxDuration)
	return ok.Val()
}

func unlock(doi string) {
	rdb.Del(context.Background(), "lock: "+doi)
}

func AddJob(job Job) error {
	if !lock(job.Doi) {
		return fmt.Errorf("Job for this dataverse is already in progress")
	}
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	cmd := rdb.LPush(context.Background(), "jobs", string(b))
	return cmd.Err()
}

func popJob() (Job, bool) {
	cmd := rdb.RPop(context.Background(), "jobs")
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
	Wait.Add(1)
	defer logging.Logger.Println("worker exited grecefully")
	defer Wait.Done()
	for {
		select {
		case <-Stop:
			return
		case <-time.After(10 * time.Second):
		}
		job, ok := popJob()
		if ok {
			err := PersistNodeMap(job)
			if err != nil {
				logging.Logger.Println("job failed:", job.Doi, err)
			}
			unlock(job.Doi)
		}
	}
}
