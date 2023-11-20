// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"integration/app/plugin/funcs/stream"
	"integration/app/plugin/types"
	"integration/app/tree"
	"sync"
	"time"
)

var FileNamesInCacheDuration = 5 * time.Minute
var deleteAndCleanupCtxDuration = 5 * time.Minute

func doWork(job Job) (Job, error) {
	ctx, cancel := context.WithDeadline(context.Background(), job.Deadline)
	defer cancel()
	go func() {
		select {
		case <-Stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if job.Plugin == "hash-only" {
		return doRehash(ctx, job.DataverseKey, job.User, job.PersistentId, job.WritableNodes, job)
	}

	job.StreamParams.Token, _ = GetTokenFromCache(ctx, job.StreamParams.Token, job.SessionId)
	streams, err := stream.Streams(ctx, job.WritableNodes, job.Plugin, job.StreamParams)
	if err != nil {
		return job, err
	}
	//filter not valid actions (when someone had browser open for a very long time and other job started and finished)
	writableNodes, err := filterRedundant(ctx, job)
	if err != nil {
		return job, err
	}

	writableNodesSlice := splitInMultipleNodeMaps(writableNodes)
	notPersisted := map[string]tree.Node{}
	writtenKeys := &[]string{}
	mutex := &sync.Mutex{}
	wg := &sync.WaitGroup{}
	for _, w := range writableNodesSlice {
		wg.Add(1)
		go doPersistNodeMapAsync(ctx, mutex, job, w, streams, writtenKeys, notPersisted, &err, wg)
	}
	wg.Wait()
	job.WritableNodes = notPersisted
	if err != nil {
		return job, err
	}

	*writtenKeys = append(*writtenKeys, fmt.Sprintf("error %v", job.PersistentId))
	err = cleanup(ctx, job.DataverseKey, job.User, job.PersistentId, *writtenKeys)
	if err != nil {
		return job, err
	}

	return job, sendJobSuccesMail(job)
}

func doPersistNodeMapAsync(ctx context.Context, mutex *sync.Mutex, job Job, w map[string]tree.Node, streams map[string]types.Stream, writtenKeys *[]string, notPersisted map[string]tree.Node, err *error, wg *sync.WaitGroup) {
	job.WritableNodes = w
	np, wk, e := doPersistNodeMap(ctx, streams, job)
	mutex.Lock()
	defer mutex.Unlock()
	*writtenKeys = append(*writtenKeys, wk...)
	for k, v := range np {
		notPersisted[k] = v
	}
	if e != nil {
		*err = e
	}
	wg.Done()
}

func splitInMultipleNodeMaps(writableNodes map[string]tree.Node) (res []map[string]tree.Node) {
	if !Destination.IsDirectUpload() {
		return []map[string]tree.Node{writableNodes}
	}
	slice := []string{}
	for k := range writableNodes {
		slice = append(slice, k)
	}
	chunk := len(writableNodes) / config.GetNbSubTasks()
	if chunk == 0 {
		chunk = 1
	}

	for i := 0; i < len(slice); i += chunk {
		end := i + chunk
		if end > len(slice) {
			end = len(slice)
		}
		m := map[string]tree.Node{}
		for _, key := range slice[i:end] {
			m[key] = writableNodes[key]
		}
		res = append(res, m)
	}

	return
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

func sendJobSuccesMail(job Job) error {
	if !job.SendEmailOnSucces {
		return nil
	}
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	to, err := Destination.GetUserEmail(shortContext, job.DataverseKey, job.User)
	if err != nil {
		return fmt.Errorf("error when sending email on succes: %v", err)
	}
	msg := fmt.Sprintf("To: %v\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\n"+
		"Subject: %v\r\n\r\n<html><body>%v</body>\r\n", to, getSubjectOnSucces(job), getContentOnSucces(job))
	err = SendMail(msg, []string{to})
	if err != nil {
		return fmt.Errorf("error when sending email on succes: %v", err)
	}
	return nil
}

func filterRedundant(ctx context.Context, job Job) (map[string]tree.Node, error) {
	filteredEqual := map[string]tree.Node{}
	isDelete := false
	knownHashes := getKnownHashes(ctx, job.PersistentId)
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

func doPersistNodeMap(ctx context.Context, streams map[string]types.Stream, in Job) (notPersisted map[string]tree.Node, writtenKeys []string, err error) {
	dataverseKey, user, persistentId, writableNodes := in.DataverseKey, in.User, in.PersistentId, in.WritableNodes
	err = Destination.CheckPermission(ctx, dataverseKey, user, persistentId)
	if err != nil {
		return
	}

	notPersisted = in.WritableNodes
	i := 0
	total := len(writableNodes)
	writtenKeys = []string{}
	toAddIdentifiers := []string{}
	toAddNodes := []tree.Node{}
	toReplaceIdentifiers := []string{}
	toReplaceNodes := []tree.Node{}

	for k, v := range writableNodes {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		i++
		if i%10 == 0 && i < total {
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}

		redisKey := fmt.Sprintf("%v -> %v", persistentId, k)
		if v.Action == tree.Delete {
			err = deleteFile(ctx, dataverseKey, user, v.Attributes.DestinationFile.Id)
			if err != nil {
				return
			}
			deleteKnownHash(ctx, persistentId, v.Id)
			delete(notPersisted, k)
			config.GetRedis().Set(ctx, redisKey, types.Deleted, FileNamesInCacheDuration)
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
		h, remoteH, size, err = write(ctx, v.Attributes.DestinationFile.Id, dataverseKey, user, fileStream, storageIdentifier, persistentId, hashType, remoteHashType, k, v.Attributes.RemoteFilesize)
		if err != nil {
			return
		}

		hashValue := fmt.Sprintf("%x", h)
		v.Attributes.DestinationFile.Hash = hashValue
		v.Attributes.DestinationFile.HashType = hashType
		v.Attributes.DestinationFile.Filesize = size

		//updated or new: always rehash
		remoteHashValue := fmt.Sprintf("%x", remoteH)
		if remoteHashType == types.GitHash {
			remoteHashValue = v.Attributes.RemoteHash // gitlab does not provide filesize... If we do not know the filesize before calculating the hash, we can't calculate the git hash
		}
		if v.Attributes.RemoteHash != remoteHashValue && v.Attributes.RemoteHash != types.NotNeeded { // not all local file system hashes are calculated on beforehand (types.NotNeeded)
			err = fmt.Errorf("downloaded file hash not equal")
			return
		}

		if Destination.IsDirectUpload() {
			if v.Attributes.DestinationFile.Id != 0 {
				toReplaceIdentifiers = append(toReplaceIdentifiers, storageIdentifier)
				toReplaceNodes = append(toReplaceNodes, v)
			} else {
				toAddIdentifiers = append(toAddIdentifiers, storageIdentifier)
				toAddNodes = append(toAddNodes, v)
			}
		}

		if hashValue != remoteHashValue {
			calculatedHash := calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: remoteHashValue},
			}
			putKnownHash(ctx, persistentId, v.Id, calculatedHash)
		}
		config.GetRedis().Set(ctx, redisKey, types.Written, FileNamesInCacheDuration)
		writtenKeys = append(writtenKeys, redisKey)

		delete(notPersisted, k)
	}

	if len(toAddNodes) > 0 || len(toReplaceNodes) > 0 {
		logging.Logger.Printf("%v: flushing added: %v replaced: %v...\n", persistentId, len(toAddNodes), len(toReplaceNodes))
		var flushed map[string]bool
		flushed, err = flush(ctx, dataverseKey, user, persistentId, toAddIdentifiers, toReplaceIdentifiers, toAddNodes, toReplaceNodes)
		if err != nil {
			rollback := toAddNodes
			rollback = append(rollback, toReplaceNodes...)
			shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
			defer cancel()
			for _, rb := range rollback {
				k := rb.Id
				if !flushed[k] {
					notPersisted[k] = rb
					deleteKnownHash(shortContext, persistentId, k)
					config.GetRedis().Del(shortContext, k)
				}
			}
			return
		}
		logging.Logger.Printf("%v: flushed\n", persistentId)
	}

	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}
	return
}

var flashMutex = sync.Mutex{}

func flush(ctx context.Context, dataverseKey, user, persistentId string, toAddIdentifiers, toReplaceIdentifiers []string, toAddNodes, toReplaceNodes []tree.Node) (res map[string]bool, err error) {
	flashMutex.Lock()
	defer flashMutex.Unlock()
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

func cleanup(ctx context.Context, token, user, persistentId string, writtenKeys []string) error {
	go cleanRedis(writtenKeys)
	return Destination.CleanupLeftOverFiles(ctx, persistentId, token, user)
}

func cleanRedis(writtenKeys []string) {
	time.Sleep(FileNamesInCacheDuration)
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	for _, k := range writtenKeys {
		config.GetRedis().Del(shortContext, k)
	}
}

func deleteFile(ctx context.Context, token, user string, id int64) error {
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	return Destination.DeleteFile(shortContext, token, user, id)
}
