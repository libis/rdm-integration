// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package core

import (
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
	debugLog := []string{}

	// Helper to append debug messages
	appendDebug := func(msg string) {
		timestamp := time.Now().Format("15:04:05")
		debugLog = append(debugLog, fmt.Sprintf("[%s] %s", timestamp, msg))
		logging.Logger.Printf("DDI-CDI [%s]: %s", job.Key, msg)
	}

	appendDebug(fmt.Sprintf("Starting DDI-CDI generation for dataset: %s", job.PersistentId))

	fileNames := make([]string, 0, len(job.WritableNodes))
	for name := range job.WritableNodes {
		fileNames = append(fileNames, name)
	}

	appendDebug(fmt.Sprintf("Processing %d files: %v", len(fileNames), fileNames))

	if len(fileNames) == 0 {
		errorMessage = "computation failed"
		consoleOut = "no data files found"
	} else {
		sort.Strings(fileNames)
		for _, name := range fileNames {
			delete(job.WritableNodes, name)
		}

		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
		if job.Deadline.IsZero() {
			ctx, cancel = context.WithCancel(context.Background())
			appendDebug("No deadline set, using cancellable context")
		} else {
			ctx, cancel = context.WithDeadline(context.Background(), job.Deadline)
			appendDebug(fmt.Sprintf("Deadline set to: %s (timeout: %v)", job.Deadline.Format(time.RFC3339), time.Until(job.Deadline)))
		}
		defer cancel()

		appendDebug("Mounting dataset storage...")
		linkedDir, nodeMap, mountErr := mountDatasetForCdi(ctx, job)
		if mountErr != nil {
			errorMessage = "computation failed"
			consoleOut = strings.Join(append(debugLog, "ERROR: Failed to mount dataset"), "\n")
			appendDebug("Mount failed")
		} else {
			appendDebug(fmt.Sprintf("Dataset mounted successfully at: %s", linkedDir))
			defer unmountCdi(job)

			workspaceDir := jobWorkDir(job)
			appendDebug(fmt.Sprintf("Creating manifest file in workspace: %s", workspaceDir))
			manifestPath, cleanupFuncs, manifestWarnings, manifestErr := createManifestFile(ctx, job, fileNames, linkedDir, nodeMap, workspaceDir)
			warnings := make([]string, 0, len(manifestWarnings))
			warnings = append(warnings, manifestWarnings...)

			if manifestErr != nil {
				errorMessage = "computation failed"
				allMessages := append(debugLog, "ERROR: Manifest creation failed")
				allMessages = append(allMessages, warnings...)
				consoleOut = strings.Join(nonEmptyStrings(allMessages), "\n\n")
				appendDebug("Manifest creation failed")
				for _, cleanup := range cleanupFuncs {
					cleanup()
				}
			} else {
				appendDebug(fmt.Sprintf("Manifest created successfully: %s", manifestPath))
				defer func() {
					for _, cleanup := range cleanupFuncs {
						cleanup()
					}
				}()

				outputFile, err := os.CreateTemp(workspaceDir, "ddi-cdi-output-*.ttl")
				if err != nil {
					errorMessage = "computation failed"
					allMessages := append(debugLog, "ERROR: Failed to create CDI output file")
					allMessages = append(allMessages, warnings...)
					consoleOut = strings.Join(nonEmptyStrings(allMessages), "\n\n")
					appendDebug("Output file creation failed")
				} else {
					outputPath := outputFile.Name()
					if closeErr := outputFile.Close(); closeErr != nil {
						os.Remove(outputPath)
						errorMessage = "computation failed"
						allMessages := append(debugLog, "ERROR: Failed to close CDI output file")
						allMessages = append(allMessages, warnings...)
						consoleOut = strings.Join(nonEmptyStrings(allMessages), "\n\n")
						appendDebug("Output file close failed")
					} else {
						defer os.Remove(outputPath)

						args := []string{
							"/usr/local/bin/cdi_generator_jsonld.py",
							"--manifest", manifestPath,
							"--output", outputPath,
							"--skip-md5",
							"--quiet",
						}

						appendDebug(fmt.Sprintf("Executing Python DDI-CDI JSON-LD generator: python3 %v", args))
						cmd := exec.CommandContext(ctx, "python3", args...)
						cmd.Dir = workspaceDir
						startTime := time.Now()
						output, cmdErr := cmd.CombinedOutput()
						duration := time.Since(startTime)
						appendDebug(fmt.Sprintf("CDI generator completed in %v", duration))

						if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
							warnings = append(warnings, "CDI generator produced diagnostic output")
							appendDebug("CDI generator produced diagnostic output")
						}
						if cmdErr != nil {
							errorMessage = "computation failed"
							allMessages := append(debugLog, "ERROR: CDI generator execution failed")
							allMessages = append(allMessages, warnings...)
							consoleOut = strings.Join(nonEmptyStrings(allMessages), "\n\n")
							appendDebug("CDI generator failed")
						} else {
							appendDebug("Reading CDI output file...")
							content, readErr := os.ReadFile(outputPath)
							if readErr != nil {
								errorMessage = "computation failed"
								allMessages := append(debugLog, "ERROR: Failed to read CDI output")
								allMessages = append(allMessages, warnings...)
								consoleOut = strings.Join(nonEmptyStrings(allMessages), "\n\n")
								appendDebug("Output file read failed")
							} else {
								ddiCdi = string(content)
								appendDebug(fmt.Sprintf("Successfully generated DDI-CDI metadata (%d bytes)", len(ddiCdi)))
								if len(warnings) > 0 {
									consoleOut = strings.Join(append(debugLog, formatWarningsAsConsoleOutput(warnings)), "\n\n")
								} else {
									consoleOut = strings.Join(debugLog, "\n")
								}
							}
						}
					}
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
		logging.Logger.Printf("Failed to send DDI-CDI notification email")
	}

	return job, nil
}

func createManifestFile(
	ctx context.Context,
	job Job,
	fileNames []string,
	linkedDir string,
	nodeMap map[string]tree.Node,
	workspaceDir string,
) (string, []func(), []string, error) {
	cleanups := make([]func(), 0)
	warnings := make([]string, 0)

	logging.Logger.Printf("DDI-CDI [%s]: Creating manifest for %d files", job.Key, len(fileNames))

	manifest := map[string]interface{}{
		"dataset_pid":      job.PersistentId,
		"dataset_uri_base": strings.TrimSuffix(config.GetExternalDestinationURL(), "/") + "/dataset",
	}

	if Destination.GetDatasetMetadata != nil {
		logging.Logger.Printf("DDI-CDI [%s]: Fetching dataset metadata from API...", job.Key)
		metadataJSON, metaErr := Destination.GetDatasetMetadata(ctx, job.PersistentId, job.DataverseKey, job.User)
		if metaErr != nil {
			warnings = append(warnings, "dataset metadata unavailable")
			logging.Logger.Printf("DDI-CDI [%s]: Dataset metadata fetch failed", job.Key)
		} else if len(metadataJSON) > 0 {
			logging.Logger.Printf("DDI-CDI [%s]: Dataset metadata retrieved (%d bytes)", job.Key, len(metadataJSON))
			tmpFile, tmpErr := os.CreateTemp(workspaceDir, "dataset-metadata-*.json")
			if tmpErr != nil {
				warnings = append(warnings, "failed to create metadata temp file")
			} else {
				if _, writeErr := tmpFile.Write(metadataJSON); writeErr != nil {
					tmpFile.Close()
					os.Remove(tmpFile.Name())
					warnings = append(warnings, "failed to write metadata")
				} else if closeErr := tmpFile.Close(); closeErr != nil {
					os.Remove(tmpFile.Name())
					warnings = append(warnings, "failed to close metadata file")
				} else {
					metadataPath := tmpFile.Name()
					manifest["dataset_metadata_path"] = metadataPath
					cleanups = append(cleanups, func() {
						os.Remove(metadataPath)
					})
					logging.Logger.Printf("DDI-CDI [%s]: Dataset metadata saved to: %s", job.Key, metadataPath)
				}
			}
		}
	}

	files := make([]map[string]interface{}, 0, len(fileNames))
	for i, fileName := range fileNames {
		logging.Logger.Printf("DDI-CDI [%s]: Processing file %d/%d: %s", job.Key, i+1, len(fileNames), fileName)
		selectedNode, ok := nodeMap[fileName]
		if !ok {
			base := filepath.Base(fileName)
			selectedNode, ok = nodeMap[base]
		}
		if !ok {
			warnings = append(warnings, "selected file not found in dataset")
			logging.Logger.Printf("DDI-CDI [%s]: selected file not found in dataset", job.Key)
			continue
		}

		csvPath := filepath.Join(linkedDir, fileName)
		if info, err := os.Stat(csvPath); err != nil {
			warnings = append(warnings, "selected file unavailable")
			logging.Logger.Printf("DDI-CDI [%s]: selected file unavailable", job.Key)
			continue
		} else if info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("file %s is a directory; skipping", fileName))
			logging.Logger.Printf("DDI-CDI [%s]: File %s is a directory, skipping", job.Key, fileName)
			continue
		} else {
			logging.Logger.Printf("DDI-CDI [%s]: File %s exists (%d bytes)", job.Key, fileName, info.Size())
		}

		entry := map[string]interface{}{
			"csv_path":        csvPath,
			"file_name":       fileName,
			"metadata_lookup": fileName,
			"allow_xconvert":  false,
		}

		allowXconvert := false
		logging.Logger.Printf("DDI-CDI [%s]: Fetching DDI metadata for file %s (fileID: %d)...", job.Key, fileName, selectedNode.Attributes.DestinationFile.Id)
		ddiPath, cleanup, ddiErr := fetchDataFileDDI(ctx, job, selectedNode, linkedDir, nodeMap)
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
		if ddiErr != nil {
			// DDI fetch failed - this is expected when ingestion is disabled
			// Allow xconvert for syntax files or direct processing for CSV/TSV/TAB
			warnings = append(warnings, "no DDI metadata from Dataverse; ingestion may be disabled")
			logging.Logger.Printf("DDI-CDI [%s]: DDI fetch failed; will try xconvert or direct processing", job.Key)
			allowXconvert = true
		} else if ddiPath != "" {
			entry["ddi_path"] = ddiPath
			logging.Logger.Printf("DDI-CDI [%s]: DDI metadata saved for %s at: %s", job.Key, fileName, ddiPath)
		} else {
			// Empty path returned but no error - unusual case
			logging.Logger.Printf("DDI-CDI [%s]: No DDI metadata returned for %s (empty path) - will try xconvert or direct processing", job.Key, fileName)
			allowXconvert = true
		}
		if allowXconvert {
			entry["allow_xconvert"] = true
		}

		files = append(files, entry)
	}

	if len(files) == 0 {
		return "", cleanups, warnings, fmt.Errorf("no valid files to process - all files were skipped")
	}

	manifest["files"] = files
	logging.Logger.Printf("DDI-CDI [%s]: Writing manifest with %d file entries", job.Key, len(files))

	tmpManifest, err := os.CreateTemp(workspaceDir, "ddi-cdi-manifest-*.json")
	if err != nil {
		return "", cleanups, warnings, fmt.Errorf("failed to create manifest file")
	}

	encoder := json.NewEncoder(tmpManifest)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		tmpManifest.Close()
		os.Remove(tmpManifest.Name())
		return "", cleanups, warnings, fmt.Errorf("failed to write manifest file")
	}

	if closeErr := tmpManifest.Close(); closeErr != nil {
		os.Remove(tmpManifest.Name())
		return "", cleanups, warnings, fmt.Errorf("failed to close manifest file")
	}

	manifestPath := tmpManifest.Name()
	cleanups = append(cleanups, func() {
		os.Remove(manifestPath)
	})

	return manifestPath, cleanups, warnings, nil
}

func fetchDataFileDDI(ctx context.Context, job Job, node tree.Node, workDir string, nodeMap map[string]tree.Node) (string, func(), error) {
	var apiErr error
	if Destination.GetDataFileDDI != nil {
		fileID := node.Attributes.DestinationFile.Id
		if fileID == 0 {
			apiErr = fmt.Errorf("data file identifier missing")
			logging.Logger.Printf("DDI-CDI [%s]: File ID is 0 for node", job.Key)
		} else {
			logging.Logger.Printf("DDI-CDI [%s]: Calling GetDataFileDDI API for fileID %d...", job.Key, fileID)
			startTime := time.Now()
			ddiBytes, err := Destination.GetDataFileDDI(ctx, job.DataverseKey, job.User, fileID)
			duration := time.Since(startTime)

			if err == nil {
				logging.Logger.Printf("DDI-CDI [%s]: GetDataFileDDI returned %d bytes in %v", job.Key, len(ddiBytes), duration)
				if err := validateDDIResponse(fileID, ddiBytes); err != nil {
					logging.Logger.Printf("DDI-CDI [%s]: DDI validation failed for fileID %d", job.Key, fileID)
					return "", nil, err
				}
				tmpFile, tmpErr := os.CreateTemp(workDir, fmt.Sprintf("ddi-%d-*.xml", fileID))
				if tmpErr != nil {
					logging.Logger.Printf("DDI-CDI [%s]: Failed to create temp DDI file", job.Key)
					return "", nil, fmt.Errorf("failed to create temp DDI file")
				}
				if _, writeErr := tmpFile.Write(ddiBytes); writeErr != nil {
					tmpFile.Close()
					os.Remove(tmpFile.Name())
					logging.Logger.Printf("DDI-CDI [%s]: Failed to write DDI temp file", job.Key)
					return "", nil, fmt.Errorf("failed to write temp DDI file")
				}
				if closeErr := tmpFile.Close(); closeErr != nil {
					os.Remove(tmpFile.Name())
					logging.Logger.Printf("DDI-CDI [%s]: Failed to close DDI temp file", job.Key)
					return "", nil, fmt.Errorf("failed to close temp DDI file")
				}
				cleanup := func() {
					if removeErr := os.Remove(tmpFile.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
						logging.Logger.Printf("compute: failed to remove DDI temp file")
					}
				}
				logging.Logger.Printf("DDI-CDI [%s]: DDI saved to temp file: %s", job.Key, tmpFile.Name())
				return tmpFile.Name(), cleanup, nil
			}
			logging.Logger.Printf("DDI-CDI [%s]: GetDataFileDDI API call failed after %v", job.Key, duration)
			apiErr = fmt.Errorf("DDI metadata request failed")
		}
	} else {
		apiErr = fmt.Errorf("DDI retrieval not supported by destination")
		logging.Logger.Printf("DDI-CDI [%s]: GetDataFileDDI is nil", job.Key)
	}

	// TODO: xconvert fallback for syntax files (future work)
	return "", nil, apiErr
}

func validateDDIResponse(fileID int64, payload []byte) error {
	trimmed := strings.TrimSpace(string(payload))
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if trimmed == "" {
		return fmt.Errorf("file %d: empty DDI metadata returned", fileID)
	}
	if strings.HasPrefix(trimmed, "{") {
		var dvResp struct {
			Status  string `json:"status"`
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &dvResp); err == nil && (dvResp.Status != "" || dvResp.Message != "") {
			return fmt.Errorf("Dataverse DDI request failed")
		}
		return fmt.Errorf("file %d: unexpected JSON DDI response", fileID)
	}
	if !strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "<?") {
		return fmt.Errorf("file %d: DDI response is not XML", fileID)
	}
	return nil
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

func nonEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}

func mountDatasetForCdi(ctx context.Context, job Job) (string, map[string]tree.Node, error) {
	linkedDir := jobLinkedDir(job)
	workDir := jobWorkDir(job)

	logging.Logger.Printf("DDI-CDI [%s]: Mounting S3 bucket...", job.Key)
	// Mount S3 bucket using shared function
	s3Dir, err := mountS3Bucket(job)
	if err != nil {
		logging.Logger.Printf("DDI-CDI [%s]: S3 mount failed", job.Key)
		return "S3 mount failed", nil, fmt.Errorf("S3 mount failed")
	}
	logging.Logger.Printf("DDI-CDI [%s]: S3 bucket mounted at: %s", job.Key, s3Dir)

	logging.Logger.Printf("DDI-CDI [%s]: Creating symlinks in: %s", job.Key, linkedDir)
	// Create symlinks using shared function
	nm, err := createSymlinks(ctx, job, s3Dir, linkedDir)
	if err != nil {
		logging.Logger.Printf("DDI-CDI [%s]: Symlink creation failed", job.Key)
		return "dataset link creation failed", nil, fmt.Errorf("dataset link creation failed")
	}
	logging.Logger.Printf("DDI-CDI [%s]: Created %d symlinks successfully", job.Key, len(nm))

	// Create work directory for CDI processing
	if err := os.RemoveAll(workDir); err != nil && !os.IsNotExist(err) {
		logging.Logger.Printf("DDI-CDI [%s]: Failed to clean work directory", job.Key)
		return "failed to clean work directory", nil, fmt.Errorf("failed to clean work directory")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		logging.Logger.Printf("DDI-CDI [%s]: Failed to create work directory", job.Key)
		return "failed to create work directory", nil, fmt.Errorf("failed to create work directory")
	}
	logging.Logger.Printf("DDI-CDI [%s]: Work directory ready: %s", job.Key, workDir)

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
		logging.Logger.Println("Failed to marshal DDI-CDI cache data")
		return
	}

	if err := config.GetRedis().Set(shortContext, cacheKey, string(jsonData), ddiCdiOutputCacheDuration).Err(); err != nil {
		logging.Logger.Println("Failed to cache DDI-CDI output")
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
		return fmt.Errorf("failed to send DDI-CDI email")
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
		return fmt.Errorf("failed to send DDI-CDI email")
	}

	logging.Logger.Printf("DDI-CDI notification email sent to %s for dataset %s", to, job.PersistentId)
	return nil
}
