// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"integration/app/config"
	"os/exec"
	"time"
)

type ComputeRequest struct {
	PersistentId         string `json:"persistentId"`
	DataverseKey         string `json:"dataverseKey"`
	Queue                string `json:"queue"`
	Executable           string `json:"executable"`
	SenSendEmailOnSucces bool   `json:"senSendEmailOnSucces"`
}

type CachedComputeResponse struct {
	Key          string `json:"key"`
	Ready        bool   `json:"ready"`
	ConsoleOut   string `json:"res"`
	ErrorMessage string `json:"err"`
}

var computeCacheMaxDuration = 5 * time.Minute

func CacheComputeResponse(res CachedComputeResponse) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	b, _ := json.Marshal(res)
	config.GetRedis().Set(ctx, res.Key, string(b), computeCacheMaxDuration)
}

func compute(job Job) (Job, error) {
	fileName := ""
	consoleOut := ""
	errorMessage := ""
	if len(job.WritableNodes) == 1 {
		for k := range job.WritableNodes {
			fileName = k
		}
		delete(job.WritableNodes, fileName)
		var err error
		consoleOut, err = doCompute(fileName, job)
		if err != nil {
			if consoleOut != "" {
				consoleOut = consoleOut + "\n\n"
			}
			consoleOut = consoleOut + err.Error()
		}
	} else {
		errorMessage = "computation failed"
		consoleOut = "file not found"
	}

	CacheComputeResponse(CachedComputeResponse{
		Key:          job.Key,
		Ready:        true,
		ConsoleOut:   consoleOut,
		ErrorMessage: errorMessage,
	})
	return job, nil
}

func doCompute(fileName string, job Job) (string, error) {
	ctx, cancel := context.WithDeadline(context.Background(), job.Deadline)
	defer cancel()
	out := ""
	// TODO: mount S3 -> https://github.com/kahing/goofys
	cmd := exec.CommandContext(ctx, fileName)
	err := cmd.Run()
	if err == nil {
		o, _ := cmd.Output()
		out = string(o)
	}
	return out, err
}
