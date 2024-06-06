// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"os/exec"
	"time"
)

type ComputeRequest struct {
	PersistentId          string `json:"persistentId"`
	DataverseKey          string `json:"dataverseKey"`
	Queue                 string `json:"queue"`
	Executable            string `json:"executable"`
	SenSendEmailOnSuccess bool   `json:"senSendEmailOnSuccess"`
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
	dir, err := mountDataset(ctx, job)
	if err != nil {
		out = dir
	} else {
		cmd := exec.CommandContext(ctx, "bash", "-c", "python "+fileName)
		cmd.Dir = dir
		o, err := cmd.CombinedOutput()
		out = string(o)
		if err != nil {
			out = out + "\n\n" + err.Error()
		}
	}
	unmount(job)
	return out, err
}

func mountDataset(ctx context.Context, job Job) (string, error) {
	s3Dir := job.Key + "/s3"
	linkedDir := job.Key + "/linked"
	b, err := exec.Command("mkdir", job.Key).CombinedOutput()
	if err != nil {
		return string(b), err
	}
	b, err = exec.Command("mkdir", s3Dir).CombinedOutput()
	if err != nil {
		return string(b), err
	}
	use_path_request_style := "use_path_request_style,"
	if !config.GetConfig().Options.S3Config.AWSPathstyle {
		use_path_request_style = ""
	}
	command := fmt.Sprintf("s3fs -o %vbucket=%v,host=\"%v\",ro %v", use_path_request_style, config.GetConfig().Options.S3Config.AWSBucket, config.GetConfig().Options.S3Config.AWSEndpoint, s3Dir)
	b, err = exec.Command("bash", "-c", command).CombinedOutput()
	if err != nil {
		return string(b), err
	}
	b, err = exec.Command("mkdir", linkedDir).CombinedOutput()
	if err != nil {
		return string(b), err
	}
	nm, err := Destination.Query(ctx, job.PersistentId, job.DataverseKey, job.User)
	if err != nil {
		return err.Error(), err
	}
	for _, n := range nm {
		identifier, err := trimProtocol(job.PersistentId)
		if err != nil {
			return err.Error(), err
		}
		filename := identifier + "/" + getStorage(n.Attributes.DestinationFile.StorageIdentifier).filename
		command = fmt.Sprintf("ln -s $(pwd)/%v $(pwd)/%v", s3Dir+"/"+filename, linkedDir+"/"+n.Id)
		b, err = exec.Command("bash", "-c", command).CombinedOutput()
		if err != nil {
			return string(b), err
		}
	}
	return linkedDir, err
}

func unmount(job Job) {
	s3Dir := job.Key + "/s3"
	linkedDir := job.Key + "/linked"
	exec.Command("rm", "-rf", linkedDir).Output()
	exec.Command("fusermount", "-uz", s3Dir).CombinedOutput()
	exec.Command("rmdir", s3Dir).Output()
	exec.Command("rmdir", job.Key).Output()
}
