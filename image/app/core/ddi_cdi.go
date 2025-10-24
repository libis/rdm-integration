// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"integration/app/tree"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ddiCdiOutputCacheDuration = 24 * time.Hour

func getDdiCdiOutputCacheKey(persistentId string) string {
	return fmt.Sprintf("ddi-cdi-output:%s", persistentId)
}

func DdiCdiGen(job Job) (Job, error) {
	ddiCdi := ""
	consoleOut := ""
	errorMessage := ""

	fileNames := make([]string, 0, len(job.WritableNodes))
	for name := range job.WritableNodes {
		fileNames = append(fileNames, name)
	}

	if len(fileNames) == 0 {
		errorMessage = "computation failed"
		consoleOut = "no writable files found"
	} else {
		sort.Strings(fileNames)

		outputs := make([]string, 0, len(fileNames))
		warnings := make([]string, 0)

		for _, fileName := range fileNames {
			delete(job.WritableNodes, fileName)
			output, fileWarnings, err := processCdiFile(fileName, job)
			warnings = append(warnings, fileWarnings...)
			if err != nil {
				warnings = append(warnings, formatComputeError(fileName, output, err))
				logging.Logger.Printf("compute: failed for %s: %v", fileName, err)
				continue
			}
			outputs = append(outputs, output)
		}

		if len(outputs) == 0 {
			errorMessage = "computation failed"
			consoleOut = joinWarnings(warnings)
			if strings.TrimSpace(consoleOut) == "" {
				consoleOut = "no CDI output generated"
			}
		} else {
			combined, combineErr := combineTurtleOutputs(job, outputs)
			if combineErr != nil {
				errorMessage = combineErr.Error()
				allMessages := []string{combineErr.Error()}
				for _, w := range warnings {
					if trimmed := strings.TrimSpace(w); trimmed != "" {
						allMessages = append(allMessages, trimmed)
					}
				}
				consoleOut = strings.Join(allMessages, "\n\n")
			} else {
				// Success: put Turtle in DdiCdi field, warnings in ConsoleOut
				ddiCdi = combined
				if len(warnings) > 0 {
					consoleOut = formatWarningsAsConsoleOutput(warnings)
				}
			}
		}
	}

	CacheComputeResponse(CachedComputeResponse{
		Key:          job.Key,
		Ready:        true,
		ConsoleOut:   consoleOut,
		DdiCdi:       ddiCdi,
		ErrorMessage: errorMessage,
	})

	// Cache the DDI-CDI output for async retrieval
	cacheDdiCdiOutput(job.PersistentId, ddiCdi, consoleOut, errorMessage)

	// Send email notification (if user opted in)
	if err := sendDdiCdiJobMail(job, ddiCdi, errorMessage); err != nil {
		logging.Logger.Printf("Failed to send DDI-CDI notification email: %v", err)
	}

	return job, nil
}

func processCdiFile(fileName string, job Job) (string, []string, error) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if job.Deadline.IsZero() {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithDeadline(context.Background(), job.Deadline)
	}
	defer cancel()

	warnings := make([]string, 0)

	workDir, nodeMap, err := mountDatasetForCdi(ctx, job)
	if err != nil {
		return workDir, warnings, err
	}
	defer unmountCdi(job)

	csvPath := filepath.Join(workDir, fileName)

	selectedNode, ok := nodeMap[fileName]
	if !ok {
		base := filepath.Base(fileName)
		selectedNode, ok = nodeMap[base]
	}
	if !ok {
		return "", warnings, fmt.Errorf("selected file %s not found in dataset", fileName)
	}

	// Fetch and save dataset metadata to file
	var metadataPath string
	if Destination.GetDatasetMetadata != nil {
		metadataJSON, metaErr := Destination.GetDatasetMetadata(ctx, job.PersistentId, job.DataverseKey, job.User)
		if metaErr != nil {
			warnings = append(warnings, fmt.Sprintf("dataset metadata unavailable: %v", metaErr))
		} else if len(metadataJSON) > 0 {
			tmpFile, tmpErr := os.CreateTemp(workDir, "dataset-metadata-*.json")
			if tmpErr != nil {
				warnings = append(warnings, fmt.Sprintf("failed to create metadata temp file: %v", tmpErr))
			} else {
				if _, writeErr := tmpFile.Write(metadataJSON); writeErr != nil {
					tmpFile.Close()
					warnings = append(warnings, fmt.Sprintf("failed to write metadata: %v", writeErr))
				} else if closeErr := tmpFile.Close(); closeErr != nil {
					warnings = append(warnings, fmt.Sprintf("failed to close metadata file: %v", closeErr))
				} else {
					metadataPath = tmpFile.Name()
					defer os.Remove(metadataPath)
				}
			}
		}
	}

	// Fetch and save DDI metadata to file
	ddiPath, cleanup, ddiErr := fetchDataFileDDI(ctx, job, selectedNode, workDir, nodeMap)
	if cleanup != nil {
		defer cleanup()
	}
	if ddiErr != nil {
		warnings = append(warnings, fmt.Sprintf("file %s: failed to retrieve DDI metadata: %v", fileName, ddiErr))
	}

	datasetURIBase := strings.TrimSuffix(config.GetExternalDestinationURL(), "/") + "/dataset"

	outputFile, err := os.CreateTemp(workDir, "ddi-cdi-*.ttl")
	if err != nil {
		return "", warnings, fmt.Errorf("failed to create CDI output file: %w", err)
	}
	outputPath := outputFile.Name()
	if closeErr := outputFile.Close(); closeErr != nil {
		os.Remove(outputPath)
		return "", warnings, fmt.Errorf("failed to close CDI output file: %w", closeErr)
	}
	defer os.Remove(outputPath)

	args := []string{
		"/usr/local/bin/csv_to_cdi.py",
		"--csv", csvPath,
		"--dataset-pid", job.PersistentId,
		"--dataset-uri-base", datasetURIBase,
		"--output", outputPath,
		"--skip-md5",
		"--quiet",
	}
	if metadataPath != "" {
		args = append(args, "--dataset-metadata-file", metadataPath)
	}
	if ddiErr == nil && ddiPath != "" {
		args = append(args, "--ddi-file", ddiPath)
	}

	cmd := exec.CommandContext(ctx, "python3", args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), warnings, fmt.Errorf("csv_to_cdi execution failed: %w", err)
	}
	if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
		warnings = append(warnings, trimmed)
	}
	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		return "", warnings, fmt.Errorf("failed to read CDI output: %w", readErr)
	}
	return string(content), warnings, nil
}

func fetchDataFileDDI(ctx context.Context, job Job, node tree.Node, workDir string, nodeMap map[string]tree.Node) (string, func(), error) {
	var apiErr error
	if Destination.GetDataFileDDI != nil {
		fileID := node.Attributes.DestinationFile.Id
		if fileID == 0 {
			apiErr = fmt.Errorf("data file identifier missing")
		} else {
			ddiBytes, err := Destination.GetDataFileDDI(ctx, job.DataverseKey, job.User, fileID)
			if err == nil {
				if len(ddiBytes) == 0 {
					return "", nil, fmt.Errorf("empty DDI metadata returned")
				}
				tmpFile, tmpErr := os.CreateTemp(workDir, fmt.Sprintf("ddi-%d-*.xml", fileID))
				if tmpErr != nil {
					return "", nil, tmpErr
				}
				if _, writeErr := tmpFile.Write(ddiBytes); writeErr != nil {
					tmpFile.Close()
					os.Remove(tmpFile.Name())
					return "", nil, writeErr
				}
				if closeErr := tmpFile.Close(); closeErr != nil {
					os.Remove(tmpFile.Name())
					return "", nil, closeErr
				}
				cleanup := func() {
					if removeErr := os.Remove(tmpFile.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
						logging.Logger.Printf("compute: failed to remove DDI temp file %s: %v", tmpFile.Name(), removeErr)
					}
				}
				return tmpFile.Name(), cleanup, nil
			}
			apiErr = err
		}
	} else {
		apiErr = fmt.Errorf("DDI retrieval not supported by destination")
	}

	// TODO: xconvert fallback for syntax files (future work)
	return "", nil, apiErr
}

func combineTurtleOutputs(job Job, docs []string) (string, error) {
	if len(docs) == 0 {
		return "", fmt.Errorf("no CDI documents to merge")
	}

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if job.Deadline.IsZero() {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithDeadline(context.Background(), job.Deadline)
	}
	defer cancel()

	script := `import sys, json
from rdflib import Graph

docs = json.load(sys.stdin)
graph = Graph()
for data in docs:
	if not isinstance(data, str) or not data.strip():
		continue
	graph.parse(data=data, format="turtle")
sys.stdout.write(graph.serialize(format="turtle"))`

	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	payload, err := json.Marshal(docs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal CDI fragments: %w", err)
	}
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("failed to merge CDI outputs: %s%w", stderr.String(), err)
		}
		return "", fmt.Errorf("failed to merge CDI outputs: %w", err)
	}
	return stdout.String(), nil
}

func appendWarnings(output string, warnings []string) string {
	trimmedWarnings := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		w := strings.TrimSpace(warning)
		if w != "" {
			trimmedWarnings = append(trimmedWarnings, w)
		}
	}
	if len(trimmedWarnings) == 0 {
		return output
	}

	var builder strings.Builder
	trimmedOutput := strings.TrimRight(output, "\n")
	if trimmedOutput != "" {
		builder.WriteString(trimmedOutput)
		builder.WriteString("\n")
	}
	builder.WriteString("# WARNINGS")
	for _, warning := range trimmedWarnings {
		lines := strings.Split(warning, "\n")
		for _, line := range lines {
			builder.WriteString("\n# ")
			builder.WriteString(line)
		}
	}
	builder.WriteString("\n")
	return builder.String()
}

func formatComputeError(fileName string, output string, err error) string {
	base := fmt.Sprintf("file %s failed: %v", fileName, err)
	extra := strings.TrimSpace(output)
	if extra == "" {
		return base
	}
	return fmt.Sprintf("%s\n%s", base, extra)
}

func formatWarningsAsConsoleOutput(warnings []string) string {
	var filtered []string
	for _, w := range warnings {
		if trimmed := strings.TrimSpace(w); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return "WARNINGS:\n" + strings.Join(filtered, "\n\n")
}

func joinWarnings(warnings []string) string {
	var filtered []string
	for _, w := range warnings {
		if trimmed := strings.TrimSpace(w); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return strings.Join(filtered, "\n\n")
}

func mountDatasetForCdi(ctx context.Context, job Job) (string, map[string]tree.Node, error) {
	linkedDir := jobLinkedDir(job)
	workDir := jobWorkDir(job)

	// Mount S3 bucket using shared function
	s3Dir, err := mountS3Bucket(job)
	if err != nil {
		return s3Dir, nil, err
	}

	// Create symlinks using shared function
	nm, err := createSymlinks(ctx, job, s3Dir, linkedDir)
	if err != nil {
		return err.Error(), nil, err
	}

	// Create work directory for CDI processing
	if err := os.RemoveAll(workDir); err != nil && !os.IsNotExist(err) {
		return fmt.Sprintf("failed to clean work directory %s: %v", workDir, err), nil, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Sprintf("failed to create work directory %s: %v", workDir, err), nil, err
	}

	return linkedDir, nm, nil
}

func unmountCdi(job Job) {
	// Use shared unmount function from compute.go
	unmount(job)
}

type DdiCdiOutputCache struct {
	DdiCdi       string `json:"ddiCdi"`
	ConsoleOut   string `json:"consoleOut"`
	ErrorMessage string `json:"errorMessage"`
	Timestamp    string `json:"timestamp"`
}

func cacheDdiCdiOutput(persistentId, ddiCdi, consoleOut, errorMessage string) {
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cacheKey := getDdiCdiOutputCacheKey(persistentId)
	cacheData := DdiCdiOutputCache{
		DdiCdi:       ddiCdi,
		ConsoleOut:   consoleOut,
		ErrorMessage: errorMessage,
		Timestamp:    time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(cacheData)
	if err != nil {
		logging.Logger.Printf("Failed to marshal DDI-CDI cache data: %v", err)
		return
	}

	if err := config.GetRedis().Set(shortContext, cacheKey, string(jsonData), ddiCdiOutputCacheDuration).Err(); err != nil {
		logging.Logger.Printf("Failed to cache DDI-CDI output: %v", err)
	}
}

func GetCachedDdiCdiOutput(persistentId string) (*DdiCdiOutputCache, error) {
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cacheKey := getDdiCdiOutputCacheKey(persistentId)
	result, err := config.GetRedis().Get(shortContext, cacheKey).Result()
	if err != nil {
		return nil, err
	}

	var cacheData DdiCdiOutputCache
	if err := json.Unmarshal([]byte(result), &cacheData); err != nil {
		return nil, err
	}

	return &cacheData, nil
}

func sendDdiCdiJobMail(job Job, ddiCdi, errorMessage string) error {
	// Only send email if user opted in via checkbox OR if there's an error (always notify on failure)
	if !job.SendEmailOnSuccess && errorMessage == "" {
		return nil
	}
	if Destination.GetUserEmail == nil {
		return nil
	}

	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	to, err := Destination.GetUserEmail(shortContext, job.DataverseKey, job.User)
	if err != nil {
		return fmt.Errorf("error when sending DDI-CDI email: %v", err)
	}

	var subject, content string
	ddiCdiURL := strings.TrimSuffix(config.GetExternalDestinationURL(), "/") + "/ddi-cdi?datasetPid=" + job.PersistentId

	if errorMessage != "" {
		subject = fmt.Sprintf("DDI-CDI Generation Failed - %s", job.PersistentId)
		content = fmt.Sprintf(
			`<h2>DDI-CDI Generation Failed</h2>
			<p>The DDI-CDI metadata generation job for dataset <strong>%s</strong> has failed.</p>
			<p><strong>Error:</strong> %s</p>
			<p>Please <a href="%s">click here</a> to view the details and try again.</p>`,
			job.PersistentId, errorMessage, ddiCdiURL)
	} else if ddiCdi != "" {
		subject = fmt.Sprintf("DDI-CDI Generation Complete - %s", job.PersistentId)
		content = fmt.Sprintf(
			`<h2>DDI-CDI Generation Complete</h2>
			<p>The DDI-CDI metadata generation job for dataset <strong>%s</strong> has completed successfully.</p>
			<p>You can now:</p>
			<ul>
				<li><a href="%s">View and edit the generated metadata</a></li>
				<li>Review the metadata in the interactive form</li>
				<li>Add the metadata file to your dataset</li>
			</ul>
			<p>The generated metadata will be available for 24 hours.</p>`,
			job.PersistentId, ddiCdiURL)
	} else {
		// No output generated but no error either
		return nil
	}

	msg := fmt.Sprintf("To: %v\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\nSubject: %v\r\n\r\n<html><body>%v</body></html>\r\n",
		to, subject, content)

	if err := SendMail(msg, []string{to}); err != nil {
		return fmt.Errorf("error when sending DDI-CDI email: %v", err)
	}

	logging.Logger.Printf("DDI-CDI notification email sent to %s for dataset %s", to, job.PersistentId)
	return nil
}
