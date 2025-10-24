// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/tree"
	"os"
	"os/exec"
	"path/filepath"
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
	DdiCdi       string `json:"ddiCdi,omitempty"` // DDI-CDI Turtle output (only for ddi_cdi plugin)
	ErrorMessage string `json:"err"`
}

var computeCacheMaxDuration = 5 * time.Minute

func workspaceRoot() string {
	root := config.GetConfig().Options.WorkspaceRoot
	if root == "" {
		root = "/dsdata"
	}
	cleaned := filepath.Clean(root)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(string(os.PathSeparator), cleaned)
	}
	return cleaned
}

func jobWorkspaceDir(job Job) string {
	return filepath.Join(workspaceRoot(), job.Key)
}

func jobS3Dir(job Job) string {
	return filepath.Join(jobWorkspaceDir(job), "s3")
}

func jobLinkedDir(job Job) string {
	return filepath.Join(jobWorkspaceDir(job), "linked")
}

func jobWorkDir(job Job) string {
	return filepath.Join(jobWorkspaceDir(job), "work")
}

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
				consoleOut = fmt.Sprintf("%s\n\n%s", consoleOut, err.Error())
			} else {
				consoleOut = err.Error()
			}
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
			out = fmt.Sprintf("%s\n\n%s", out, err.Error())
		}
	}
	unmount(job)
	return out, err
}

// mountS3Bucket mounts the S3 bucket and creates base directories
func mountS3Bucket(job Job) (s3Dir string, err error) {
	s3Dir = jobS3Dir(job)
	workspaceDir := jobWorkspaceDir(job)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Sprintf("failed to create workspace %s: %v", workspaceDir, err), err
	}
	if err := os.MkdirAll(s3Dir, 0o755); err != nil {
		return fmt.Sprintf("failed to create s3 mountpoint %s: %v", s3Dir, err), err
	}
	use_path_request_style := "use_path_request_style,"
	if !config.GetConfig().Options.S3Config.AWSPathstyle {
		use_path_request_style = ""
	}
	command := fmt.Sprintf("s3fs -o %vbucket=%v,host=\"%v\",ro %v", use_path_request_style, config.GetConfig().Options.S3Config.AWSBucket, config.GetConfig().Options.S3Config.AWSEndpoint, s3Dir)
	if output, err := exec.Command("bash", "-c", command).CombinedOutput(); err != nil {
		return string(output), err
	}
	return s3Dir, nil
}

// createSymlinks creates symlinks for dataset files in the target directory
func createSymlinks(ctx context.Context, job Job, s3Dir, targetDir string) (map[string]tree.Node, error) {
	if err := os.RemoveAll(targetDir); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to clean target directory %s: %w", targetDir, err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}
	nm, err := Destination.Query(ctx, job.PersistentId, job.DataverseKey, job.User)
	if err != nil {
		return nil, err
	}
	identifier, err := trimProtocol(job.PersistentId)
	if err != nil {
		return nil, err
	}
	for _, n := range nm {
		storage := getStorage(n.Attributes.DestinationFile.StorageIdentifier)
		relativePath := fmt.Sprintf("%s/%s", identifier, storage.filename)
		sourcePath := filepath.Join(s3Dir, relativePath)
		targetPath := filepath.Join(targetDir, n.Id)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to prepare target directory for %s: %w", targetPath, err)
		}
		if err := os.Symlink(sourcePath, targetPath); err != nil {
			if os.IsExist(err) {
				if removeErr := os.Remove(targetPath); removeErr != nil && !os.IsNotExist(removeErr) {
					return nil, fmt.Errorf("failed to replace existing symlink %s: %w", targetPath, removeErr)
				}
				if err = os.Symlink(sourcePath, targetPath); err != nil {
					return nil, fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, sourcePath, err)
				}
			} else {
				return nil, fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, sourcePath, err)
			}
		}
	}
	return nm, nil
}

func mountDataset(ctx context.Context, job Job) (string, error) {
	linkedDir := jobLinkedDir(job)
	s3Dir, err := mountS3Bucket(job)
	if err != nil {
		return s3Dir, err
	}
	_, err = createSymlinks(ctx, job, s3Dir, linkedDir)
	if err != nil {
		return err.Error(), err
	}
	return linkedDir, nil
}

func unmount(job Job) {
	if job.Key == "" {
		return
	}
	s3Dir := jobS3Dir(job)
	linkedDir := jobLinkedDir(job)
	workDir := jobWorkDir(job)
	workspaceDir := jobWorkspaceDir(job)
	os.RemoveAll(linkedDir)
	os.RemoveAll(workDir)
	exec.Command("fusermount", "-uz", s3Dir).CombinedOutput()
	os.RemoveAll(s3Dir)
	if err := os.Remove(workspaceDir); err != nil && !os.IsNotExist(err) {
		// best-effort cleanup; ignore errors to match previous behavior
	}
}
