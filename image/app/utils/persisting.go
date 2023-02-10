// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import (
	"context"
	"fmt"
	dv "integration/app/dataverse"
	"integration/app/logging"
	"integration/app/plugin/funcs/stream"
	"integration/app/plugin/types"
	"integration/app/tree"
	"time"
)

var fileNamesInCacheDuration = 1 * time.Minute

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
	return doPersistNodeMap(ctx, streams, job, knownHashes)
}

func filterRedundant(ctx context.Context, job Job, knownHashes map[string]calculatedHashes) (map[string]tree.Node, error) {
	filteredEqual := map[string]tree.Node{}
	isDelete := false
	for k, v := range job.WritableNodes {
		localHash := knownHashes[k].LocalHashValue
		h, ok := knownHashes[k].RemoteHashes[v.Attributes.RemoteHashType]
		if v.Action == tree.Delete {
			isDelete = true
		} else if ok && h == v.Attributes.RemoteHash && localHash == v.Attributes.LocalHash {
			continue
		}
		filteredEqual[k] = v
	}
	if !isDelete {
		return filteredEqual, nil
	}
	res := map[string]tree.Node{}
	nm, err := GetNodeMap(ctx, job.PersistentId, job.DataverseKey, job.User)
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
	err = CheckPermission(ctx, dataverseKey, user, persistentId)
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
			if nativeApiDelete != "true" {
				err = swordDelete(ctx, dataverseKey, user, v.Attributes.Metadata.DataFile.Id)
			} else {
				err = deleteFile(ctx, dataverseKey, user, v.Attributes.Metadata.DataFile.Id)
			}
			if err != nil {
				return
			}
			delete(knownHashes, v.Id)
			delete(out.WritableNodes, k)
			GetRedis().Set(ctx, redisKey, types.Deleted, fileNamesInCacheDuration)
			writtenKeys = append(writtenKeys, redisKey)
			continue
		}

		fileStream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := config.Options.DefaultHash
		remoteHashType := v.Attributes.RemoteHashType

		var h []byte
		var remoteH []byte
		var size int64
		h, remoteH, size, err = write(ctx, v.Attributes.Metadata.DataFile.Id, dataverseKey, user, fileStream, storageIdentifier, persistentId, hashType, remoteHashType, k, v.Attributes.Metadata.DataFile.Filesize)
		if err != nil {
			return
		}

		v.Attributes.Metadata.DataFile.Filesize = size
		hashValue := fmt.Sprintf("%x", h)
		//updated or new: always rehash
		remoteHashVlaue := fmt.Sprintf("%x", remoteH)
		if remoteHashType == types.GitHash {
			remoteHashVlaue = v.Attributes.RemoteHash // gitlab does not provide filesize... If we do not know the filesize before calculating the hash, we can't calculate the git hash
		}
		if v.Attributes.RemoteHash != remoteHashVlaue && v.Attributes.RemoteHash != types.NotNeeded { // not all local file system hashes are calculated on beforehand (types.NotNeeded)
			err = fmt.Errorf("downloaded file hash not equal")
			return
		}

		if directUpload == "true" && config.Options.DefaultDriver != "" {
			directoryLabel := v.Attributes.Metadata.DirectoryLabel
			jsonData := dv.JsonData{
				FileToReplaceId:   v.Attributes.Metadata.DataFile.Id,
				ForceReplace:      v.Attributes.Metadata.DataFile.Id != 0,
				StorageIdentifier: storageIdentifier,
				FileName:          v.Attributes.Metadata.DataFile.Filename,
				DirectoryLabel:    directoryLabel,
				MimeType:          "application/octet-stream", // default that will be replaced by Dataverse while adding/replacing the file
				Checksum: &dv.Checksum{
					Type:  hashType,
					Value: hashValue,
				},
			}
			err = directAddReplaceFile(ctx, dataverseKey, user, persistentId, jsonData)
			if err != nil {
				return
			}
		} else {
			var nm map[string]tree.Node
			fileFound := false
			written := tree.Node{}
			for i := 0; !fileFound && i < 5; i++ {
				nm, err = GetNodeMap(ctx, persistentId, dataverseKey, user)
				if err != nil {
					return
				}
				written, fileFound = nm[k]
				if !fileFound {
					time.Sleep(time.Second)
				}
			}
			if !fileFound {
				err = fmt.Errorf("file is written but not found back")
				return
			}
			v.Attributes.Metadata.DataFile.Id = written.Attributes.Metadata.DataFile.Id
		}

		var newH []byte
		newH, err = doHash(ctx, dataverseKey, user, persistentId, v)
		if err != nil {
			return
		}
		if remoteHashVlaue != fmt.Sprintf("%x", newH) {
			err = fmt.Errorf("written file hash not equal")
		}

		if hashValue != remoteHashVlaue {
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: remoteHashVlaue},
			}
		}
		GetRedis().Set(ctx, redisKey, types.Written, fileNamesInCacheDuration)
		writtenKeys = append(writtenKeys, redisKey)

		delete(out.WritableNodes, k)
	}
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
		err = cleanup(ctx, in.DataverseKey, in.User, in.PersistentId, writtenKeys)
	}
	return
}
