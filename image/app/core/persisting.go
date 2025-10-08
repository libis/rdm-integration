// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/core/oauth"
	"integration/app/logging"
	"integration/app/plugin/funcs/stream"
	"integration/app/plugin/types"
	"integration/app/tree"
	"strings"
	"time"
)

var FileNamesInCacheDuration = 5 * time.Minute
var deleteAndCleanupCtxDuration = 5 * time.Minute

func DoWork(job Job) (Job, error) {
	ctx, cancel := context.WithDeadline(context.Background(), job.Deadline)
	defer cancel()

	// Use a buffered channel for graceful shutdown
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-Stop:
			cancel()
		case <-ctx.Done():
		case <-done:
		}
	}()

	if job.Plugin == "hash-only" {
		return doRehash(ctx, job.DataverseKey, job.User, job.PersistentId, job.WritableNodes, job)
	}
	knownHashes := getKnownHashes(ctx, job.PersistentId)
	//filter not valid actions (when someone had browser open for a very long time and other job started and finished)
	writableNodes, err := filterRedundant(ctx, job, knownHashes)
	if err != nil {
		return job, err
	}
	job.WritableNodes = writableNodes
	streamNodes := map[string]tree.Node{}
	for k, v := range writableNodes {
		if v.Action != tree.Delete {
			streamNodes[k] = v
		}
	}
	streamParams := job.StreamParams
	streamParams.Token = oauth.GetTokenFromCache(ctx, job.StreamParams.Token, job.SessionId, job.StreamParams.PluginId)
	streamParams.PersistentId = job.PersistentId
	streamParams.DVToken = job.DataverseKey
	streamParams.SessionId = job.SessionId
	streams, err := stream.Streams(ctx, streamNodes, job.Plugin, streamParams)
	if err != nil {
		return job, err
	}
	j, err := doPersistNodeMap(ctx, streams.Streams, job, knownHashes)
	if err != nil {
		if streams.Cleanup != nil {
			if cleanupErr := streams.Cleanup(); cleanupErr != nil {
				logging.Logger.Println("cleanup failed:", cleanupErr)
			}
		}
		return j, err
	}
	if streams.Cleanup != nil {
		if cleanupErr := streams.Cleanup(); cleanupErr != nil {
			if job.Plugin == "globus" && strings.HasPrefix(cleanupErr.Error(), "globus transfer started, task ID: ") {
				j.GlobusTaskId = strings.TrimPrefix(cleanupErr.Error(), "globus transfer started, task ID: ")
			} else {
				return j, cleanupErr
			}
		}
	}
	return j, sendJobSuccessMail(j)
}

func sendJobFailedMail(errIn error, job Job) error {
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	config.GetRedis().Set(shortContext, fmt.Sprintf("error %v", job.PersistentId), errIn.Error(), FileNamesInCacheDuration)
	to, err := Destination.GetUserEmail(shortContext, job.DataverseKey, job.User)
	if err != nil {
		return fmt.Errorf("error when sending email on error (%v): %v", errIn, err)
	}
	msg := fmt.Sprintf("To: %v\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\nSubject: %v"+
		"\r\n\r\n<html><body>%v</body></html>\r\n", to, getSubjectOnError(errIn, job), getContentOnError(errIn, job))
	err = SendMail(msg, []string{to})
	if err != nil {
		return fmt.Errorf("error when sending email on error (%v): %v", errIn, err)
	}
	return errIn
}

func sendJobSuccessMail(job Job) error {
	if !job.SendEmailOnSuccess {
		return nil
	}
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	to, err := Destination.GetUserEmail(shortContext, job.DataverseKey, job.User)
	if err != nil {
		return fmt.Errorf("error when sending email on success: %v", err)
	}
	msg := fmt.Sprintf("To: %v\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\n"+
		"Subject: %v\r\n\r\n<html><body>%v</body>\r\n", to, getSubjectOnSuccess(job), getContentOnSuccess(job))
	err = SendMail(msg, []string{to})
	if err != nil {
		return fmt.Errorf("error when sending email on success: %v", err)
	}
	return nil
}

func filterRedundant(ctx context.Context, job Job, knownHashes map[string]calculatedHashes) (map[string]tree.Node, error) {
	filteredEqual := map[string]tree.Node{}
	isDelete := false
	for k, v := range job.WritableNodes {
		localHash := knownHashes[k].LocalHashValue
		h, ok := knownHashes[k].RemoteHashes[v.Attributes.RemoteHashType]
		if v.Action == tree.Delete {
			isDelete = true
		} else if ok && h == v.Attributes.RemoteHash && localHash == v.Attributes.DestinationFile.Hash {
			continue
		}
		filteredEqual[k] = v
	}
	if !isDelete {
		return filteredEqual, nil
	}
	res := map[string]tree.Node{}
	nm, err := Destination.Query(ctx, job.PersistentId, job.DataverseKey, job.User)
	if err != nil {
		return nil, err
	}
	for k, v := range filteredEqual {
		_, ok := nm[k]
		if v.Action == tree.Delete && !ok {
			continue
		}
		res[k] = v
	}
	return res, nil
}

func doPersistNodeMap(ctx context.Context, streams map[string]types.Stream, in Job, knownHashes map[string]calculatedHashes) (out Job, err error) {
	dataverseKey, user, persistentId, writableNodes := in.DataverseKey, in.User, in.PersistentId, in.WritableNodes
	err = Destination.CheckPermission(ctx, dataverseKey, user, persistentId)
	if err != nil {
		return
	}
	defer storeKnownHashes(ctx, persistentId, knownHashes)

	out = in
	i := 0
	total := len(writableNodes)
	writtenKeys := []string{}
	toAddIdentifiers := &[]string{}
	toAddNodes := &[]tree.Node{}
	toReplaceIdentifiers := &[]string{}
	toReplaceNodes := &[]tree.Node{}
	defer doFlush(ctx, toAddNodes, toReplaceNodes, &out, knownHashes, toAddIdentifiers, toReplaceIdentifiers)

	for k, v := range writableNodes {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(ctx, persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}

		redisKey := fmt.Sprintf("%v -> %v", persistentId, k)
		if v.Action == tree.Delete {
			err = deleteFile(ctx, dataverseKey, user, v.Attributes.DestinationFile.Id)
			if err != nil {
				return
			}
			delete(knownHashes, v.Id)
			delete(out.WritableNodes, k)
			config.GetRedis().Set(ctx, redisKey, types.Deleted, FileNamesInCacheDuration)
			writtenKeys = append(writtenKeys, redisKey)
			continue
		}

		if in.Plugin == "globus" {
			if v.Action == tree.Update {
				err = deleteFile(ctx, dataverseKey, user, v.Attributes.DestinationFile.Id)
				if err != nil {
					return
				}
			}
			delete(out.WritableNodes, k)
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  types.LastModified,
				LocalHashValue: v.Attributes.RemoteHash,
				RemoteHashes:   map[string]string{types.LastModified: v.Attributes.RemoteHash},
			}
			config.GetRedis().Set(ctx, redisKey, types.Written, FileNamesInCacheDuration)
			writtenKeys = append(writtenKeys, redisKey)
			continue
		}

		fileStream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := config.GetConfig().Options.DefaultHash
		remoteHashType := v.Attributes.RemoteHashType

		var h []byte
		var remoteH []byte
		var size int64
		h, remoteH, size, err = write(ctx, v.Attributes.DestinationFile.Id, dataverseKey, user, fileStream, storageIdentifier, persistentId, hashType, remoteHashType, k, v.Attributes.RemoteFileSize)
		if err != nil {
			return
		}

		hashValue := fmt.Sprintf("%x", h)
		v.Attributes.DestinationFile.Hash = hashValue
		v.Attributes.DestinationFile.HashType = hashType
		v.Attributes.DestinationFile.FileSize = size

		//updated or new: always rehash
		remoteHashValue := fmt.Sprintf("%x", remoteH)
		if remoteHashType == types.GitHash || remoteHashType == types.LastModified {
			// gitlab does not provide filesize... If we do not know the filesize before calculating the hash, we can't calculate the git hash
			// we also cannot calculate the last modified in the file system...
			remoteHashValue = v.Attributes.RemoteHash
		}
		if v.Attributes.RemoteHash != remoteHashValue && v.Attributes.RemoteHash != types.NotNeeded { // not all local file system hashes are calculated on beforehand (types.NotNeeded)
			if remoteHashType == types.QuickXorHash { //some sharepoint hashes fail
				logging.Logger.Println("WARNING: quickXorHash not equal, expected", v.Attributes.RemoteHash, "got", remoteHashValue)
				remoteHashValue = v.Attributes.RemoteHash
			} else {
				err = fmt.Errorf("downloaded file hash not equal")
				return
			}
		}

		if Destination.IsDirectUpload() {
			if v.Attributes.DestinationFile.Id != 0 {
				*toReplaceIdentifiers = append(*toReplaceIdentifiers, storageIdentifier)
				*toReplaceNodes = append(*toReplaceNodes, v)
			} else {
				*toAddIdentifiers = append(*toAddIdentifiers, storageIdentifier)
				*toAddNodes = append(*toAddNodes, v)
			}
		}

		if hashValue != remoteHashValue {
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: remoteHashValue},
			}
		}
		config.GetRedis().Set(ctx, redisKey, types.Written, FileNamesInCacheDuration)
		writtenKeys = append(writtenKeys, redisKey)

		delete(out.WritableNodes, k)
	}

	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
		writtenKeys = append(writtenKeys, fmt.Sprintf("error %v", in.PersistentId))
		//err = cleanup(ctx, in.DataverseKey, in.User, in.PersistentId, writtenKeys)
		err = cleanup(writtenKeys)
	}
	return
}

func doFlush(ctx context.Context, toAddNodes *[]tree.Node, toReplaceNodes *[]tree.Node, job *Job, knownHashes map[string]calculatedHashes, toAddIdentifiers, toReplaceIdentifiers *[]string) {
	if len(*toAddNodes) > 0 || len(*toReplaceNodes) > 0 {
		logging.Logger.Printf("%v: flushing added: %v replaced: %v...\n", job.PersistentId, len(*toAddNodes), len(*toReplaceNodes))
		flushed, err := flush(ctx, job.DataverseKey, job.User, job.PersistentId, *toAddIdentifiers, *toReplaceIdentifiers, *toAddNodes, *toReplaceNodes)
		if err != nil {
			rollback := *toAddNodes
			rollback = append(rollback, *toReplaceNodes...)
			shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
			defer cancel()
			for _, rb := range rollback {
				k := rb.Id
				if !flushed[k] {
					job.WritableNodes[k] = rb
					delete(knownHashes, k)
					config.GetRedis().Del(shortContext, k)
				}
			}
		}
		*toAddNodes = []tree.Node{}
		*toAddIdentifiers = []string{}
		*toReplaceNodes = []tree.Node{}
		*toReplaceIdentifiers = []string{}
		logging.Logger.Printf("%v: flushed\n", job.PersistentId)
	}
}

func flush(ctx context.Context, dataverseKey, user, persistentId string, toAddIdentifiers, toReplaceIdentifiers []string, toAddNodes, toReplaceNodes []tree.Node) (res map[string]bool, err error) {
	res = make(map[string]bool)
	if len(toAddNodes) > 0 {
		err = Destination.SaveAfterDirectUpload(ctx, false, dataverseKey, user, persistentId, toAddIdentifiers, toAddNodes)
		if err != nil {
			return
		}
		for _, node := range toAddNodes {
			res[node.Id] = true
		}
	}
	if len(toReplaceNodes) > 0 {
		err = Destination.SaveAfterDirectUpload(ctx, true, dataverseKey, user, persistentId, toReplaceIdentifiers, toReplaceNodes)
		if err != nil {
			return
		}
		for _, node := range toReplaceNodes {
			res[node.Id] = true
		}
	}
	return
}

// func cleanup(ctx context.Context, token, user, persistentId string, writtenKeys []string) error {
func cleanup(writtenKeys []string) error {
	go cleanRedis(writtenKeys)
	return nil
	//return Destination.CleanupLeftOverFiles(ctx, persistentId, token, user)
}

func cleanRedis(writtenKeys []string) {
	// Reduced cleanup delay for better memory management
	time.Sleep(10 * time.Second) // Reduced from 30 seconds to 10 seconds
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()

	// Batch delete with chunking for very large key sets
	const chunkSize = 100
	for i := 0; i < len(writtenKeys); i += chunkSize {
		end := i + chunkSize
		if end > len(writtenKeys) {
			end = len(writtenKeys)
		}
		chunk := writtenKeys[i:end]
		if len(chunk) > 0 {
			config.GetRedis().Del(shortContext, chunk...)
		}
	}
}

func deleteFile(_ context.Context, token, user string, id int64) error {
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	return Destination.DeleteFile(shortContext, token, user, id)
}
