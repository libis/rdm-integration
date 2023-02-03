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

func doWork(job Job) (Job, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-Stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if job.Plugin == "hash-only" {
		return doRehash(ctx, job.DataverseKey, job.PersistentId, job.WritableNodes, job)
	}
	streams, err := stream.Streams(ctx, job.WritableNodes, job.Plugin, job.StreamParams)
	if err != nil {
		return job, err
	}
	knownHashes := getKnownHashes(job.PersistentId)
	//filter not valid actions (when someone had browser open for a very long time and other job started and finished)
	writableNodes, err := filterRedundant(job, knownHashes)
	if err != nil {
		return job, err
	}
	job.WritableNodes = writableNodes
	return doPersistNodeMap(ctx, streams, job, knownHashes)
}

func filterRedundant(job Job, knownHashes map[string]calculatedHashes) (map[string]tree.Node, error) {
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
	nm, err := GetNodeMap(job.PersistentId, job.DataverseKey)
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
	dataverseKey, persistentId, writableNodes := in.DataverseKey, in.PersistentId, in.WritableNodes
	err = CheckPermission(dataverseKey, persistentId)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			storeKnownHashes(persistentId, knownHashes)
		}
	}()
	out = in
	i := 0
	total := len(writableNodes)
	for k, v := range writableNodes {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}

		if v.Action == tree.Delete {
			err = deleteFromDV(dataverseKey, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return
			}
			delete(knownHashes, v.Id)
			delete(out.WritableNodes, k)
			GetRedis().Set(ctx, fmt.Sprintf("%v -> %v", persistentId, k), types.Deleted, time.Minute)
			continue
		}
		// delete previous version before writting new version when replacing
		if v.Attributes.Metadata.DataFile.Id != 0 {
			err = deleteFromDV(dataverseKey, v.Attributes.Metadata.DataFile.Id)
			if err != nil {
				return
			}
		}
		fileStream := streams[k]
		fileName := generateFileName()
		storageIdentifier := generateStorageIdentifier(fileName)
		hashType := config.Options.DefaultHash
		remoteHashType := v.Attributes.RemoteHashType
		var h []byte
		var remoteH []byte
		var size int
		h, remoteH, size, err = write(ctx, dataverseKey, fileStream, storageIdentifier, persistentId, hashType, remoteHashType, k, v.Attributes.Metadata.DataFile.Filesize)
		if err != nil {
			return
		}
		v.Attributes.Metadata.DataFile.Filesize = size;
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
			directoryLabel := &(v.Attributes.Metadata.DirectoryLabel)
			if *directoryLabel == "" {
				directoryLabel = nil
			}
			data := dv.JsonData{
				StorageIdentifier: storageIdentifier,
				FileName:          v.Attributes.Metadata.DataFile.Filename,
				DirectoryLabel:    directoryLabel,
				MimeType:          "application/octet-stream",
				Checksum: dv.Checksum{
					Type:  hashType,
					Value: hashValue,
				},
			}
			err = writeToDV(dataverseKey, persistentId, data)
			if err != nil {
				return
			}
		} else {
			nm := map[string]tree.Node{}
			fileFound := false
			written := tree.Node{}
			for i := 0; !fileFound && i < 5; i++ {
				nm, err = GetNodeMap(persistentId, dataverseKey)
				if err != nil {
					return
				}
				written, fileFound = nm[v.Id]
				time.Sleep(3*time.Second)
			}
			if !fileFound {
				err = fmt.Errorf("file is written but not found back")
				return
			}
			v.Attributes.Metadata.DataFile.Id  = written.Attributes.Metadata.DataFile.Id;
		}

		newH := []byte{}
		newH, err = doHash(ctx, dataverseKey, persistentId, v)
		if err != nil {
			return
		}
		if v.Attributes.RemoteHash != fmt.Sprintf("%x", newH) {
			err = fmt.Errorf("written file hash not equal")
		}

		if hashValue != remoteHashVlaue {
			knownHashes[v.Id] = calculatedHashes{
				LocalHashType:  hashType,
				LocalHashValue: hashValue,
				RemoteHashes:   map[string]string{remoteHashType: v.Attributes.RemoteHash},
			}
		}
		GetRedis().Set(ctx, fmt.Sprintf("%v -> %v", persistentId, k), types.Written, time.Minute)

		delete(out.WritableNodes, k)
	}
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
		err = cleanup(in.DataverseKey, in.PersistentId)
	}
	return
}
