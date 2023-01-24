package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"integration/app/tree"
)

type calculatedHashes struct {
	LocalHashType  string
	LocalHashValue string
	RemoteHashes   map[string]string
}

func localRehashToMatchRemoteHashType(dataverseKey, persistentId string, nodes map[string]tree.Node, addJobs bool) (map[string]tree.Node, bool) {
	knownHashes := getKnownHashes(persistentId)
	jobNodes := map[string]tree.Node{}
	res := map[string]tree.Node{}
	for k, node := range nodes {
		if node.Attributes.RemoteHashType != "" {
			value, ok := knownHashes[node.Id].RemoteHashes[node.Attributes.RemoteHashType]
			if node.Attributes.LocalHash != "" && node.Attributes.RemoteHashType == node.Attributes.Metadata.DataFile.Checksum.Type {
				value, ok = node.Attributes.LocalHash, true
			}
			redisKey := fmt.Sprintf("%v -> %v", persistentId, k)
			redisValue := GetRedis().Get(context.Background(), redisKey).Val()
			if redisValue == types.Written {
				value, ok = node.Attributes.RemoteHash, true
			}
			if redisValue == types.Deleted {
				value, ok = "", false
			}
			if redisValue != "" {
				GetRedis().Del(context.Background(), redisKey)
			}
			if !ok && node.Attributes.LocalHash != "" {
				jobNodes[k] = node
				value = "?"
			}
			node.Attributes.LocalHash = value
		}
		res[k] = node
	}
	if len(jobNodes) > 0 && addJobs {
		AddJob(
			Job{
				DataverseKey:  dataverseKey,
				PersistentId:  persistentId,
				WritableNodes: jobNodes,
				StreamType:    "hash-only",
			},
		)
	}
	return res, len(jobNodes) > 0
}

func doRehash(ctx context.Context, dataverseKey, persistentId string, nodes map[string]tree.Node, in Job) (out Job, err error) {
	err = CheckPermission(dataverseKey, persistentId)
	if err != nil {
		return
	}
	knownHashes := getKnownHashes(persistentId)
	defer func() {
		storeKnownHashes(persistentId, knownHashes)
	}()
	out = in
	i := 0
	total := len(nodes)
	for k, node := range nodes {
		err = calculateHash(ctx, dataverseKey, persistentId, node, knownHashes)
		if err != nil {
			return
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}
		delete(out.WritableNodes, k)
	}
	return
}

func getKnownHashes(persistentId string) map[string]calculatedHashes {
	res := map[string]calculatedHashes{}
	cache := GetRedis().Get(context.Background(), "hashes: "+persistentId)
	err := json.Unmarshal([]byte(cache.Val()), &res)
	if err != nil {
		return map[string]calculatedHashes{}
	}
	return res
}

func storeKnownHashes(persistentId string, knownHashes map[string]calculatedHashes) {
	knownHashesJson, err := json.Marshal(knownHashes)
	if err != nil {
		logging.Logger.Println("marshalling hashes failed")
		return
	}
	GetRedis().Set(context.Background(), "hashes: "+persistentId, string(knownHashesJson), 0)
}

func invalidateKnownHashes(persistentId string) {
	GetRedis().Del(context.Background(), "hashes: "+persistentId)
}

func calculateHash(ctx context.Context, dataverseKey, persistentId string, node tree.Node, knownHashes map[string]calculatedHashes) error {
	hashType := node.Attributes.RemoteHashType
	known, ok := knownHashes[node.Id]
	if ok && known.LocalHashType == node.Attributes.Metadata.DataFile.Checksum.Type && known.LocalHashValue == node.Attributes.Metadata.DataFile.Checksum.Value {
		_, ok2 := known.RemoteHashes[hashType]
		if ok2 {
			return nil
		}
	} else {
		known = calculatedHashes{
			LocalHashType:  node.Attributes.Metadata.DataFile.Checksum.Type,
			LocalHashValue: node.Attributes.Metadata.DataFile.Checksum.Value,
			RemoteHashes:   map[string]string{},
		}
	}
	h, err := doHash(ctx, dataverseKey, persistentId, node)
	if err != nil {
		return fmt.Errorf("failed to hash local file %v: %v", node.Attributes.Metadata.DataFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return nil
}
