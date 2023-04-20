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
	knownHashes := getKnownHashes(ctx, job.PersistentId)
	//filter not valid actions (when someone had browser open for a very long time and other job started and finished)
	writableNodes, err := filterRedundant(ctx, job, knownHashes)
	if err != nil {
		return job, err
	}
	job.WritableNodes = writableNodes
	j, err := doPersistNodeMap(ctx, streams, job, knownHashes)
	if err != nil {
		return j, err
	}
	return j, sendJobSuccesMail(j)
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
			deleteFile(ctx, dataverseKey, user, v.Attributes.DestinationFile.Id)
			delete(knownHashes, v.Id)
			delete(out.WritableNodes, k)
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
		remoteHashVlaue := fmt.Sprintf("%x", remoteH)
		if remoteHashType == types.GitHash {
			remoteHashVlaue = v.Attributes.RemoteHash // gitlab does not provide filesize... If we do not know the filesize before calculating the hash, we can't calculate the git hash
		}
		if v.Attributes.RemoteHash != remoteHashVlaue && v.Attributes.RemoteHash != types.NotNeeded { // not all local file system hashes are calculated on beforehand (types.NotNeeded)
			err = fmt.Errorf("downloaded file hash not equal")
			return
		}

		if Destination.IsDirectUpload() {
			err = Destination.SaveAfterDirectUpload(ctx, dataverseKey, user, persistentId, storageIdentifier, v)
			if err != nil {
				return
			}
		} /*else {
			var nm map[string]tree.Node
			fileFound := false
			written := tree.Node{}
			sleep := 2
			for i := 0; !fileFound && i < 5; i++ {
				nm, err = Destination.Query(ctx, persistentId, dataverseKey, user)
				if err != nil {
					return
				}
				written, fileFound = nm[k]
				if !fileFound {
					time.Sleep(time.Duration(sleep * int(time.Second)))
					sleep = sleep * 2
				}
			}
			if !fileFound {
				err = fmt.Errorf("file is written but not found back")
				return
			}
			v.Attributes.DestinationFile.Id = written.Attributes.DestinationFile.Id
		}

		var newH []byte
		newH, err = doHash(ctx, dataverseKey, user, persistentId, v)
		if err != nil {
			return
		}
		if remoteHashVlaue != fmt.Sprintf("%x", newH) {
			err = fmt.Errorf("written file hash not equal")
			return
		}*/

		if hashValue != remoteHashVlaue {
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: remoteHashVlaue},
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
		err = cleanup(ctx, in.DataverseKey, in.User, in.PersistentId, writtenKeys)
	}
	return
}

func cleanup(ctx context.Context, token, user, persistentId string, writtenKeys []string) error {
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	go cleanRedis(shortContext, writtenKeys)
	return Destination.CleanupLeftOverFiles(ctx, persistentId, token, user)
}

func cleanRedis(ctx context.Context, writtenKeys []string) {
	time.Sleep(FileNamesInCacheDuration)
	for _, k := range writtenKeys {
		config.GetRedis().Del(ctx, k)
	}
}

func deleteFile(ctx context.Context, token, user string, id int64) error {
	shortContext, cancel := context.WithTimeout(context.Background(), deleteAndCleanupCtxDuration)
	defer cancel()
	return Destination.DeleteFile(shortContext, token, user, id)
}
