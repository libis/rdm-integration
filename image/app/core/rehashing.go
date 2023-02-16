// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"integration/app/plugin/types"
	"integration/app/tree"
)

type calculatedHashes struct {
	LocalHashType  string
	LocalHashValue string
	RemoteHashes   map[string]string
}

func localRehashToMatchRemoteHashType(ctx context.Context, dataverseKey, user, persistentId string, nodes map[string]tree.Node, addJobs bool) (map[string]tree.Node, bool) {
	knownHashes := getKnownHashes(ctx, persistentId)
	jobNodes := map[string]tree.Node{}
	res := map[string]tree.Node{}
	for k, node := range nodes {
		if node.Attributes.RemoteHashType != "" {
			value, ok := knownHashes[node.Id].RemoteHashes[node.Attributes.RemoteHashType]
			if node.Attributes.DestinationFile.Hash != "" && node.Attributes.RemoteHashType == node.Attributes.DestinationFile.HashType {
				value, ok = node.Attributes.DestinationFile.Hash, true
			}
			redisKey := fmt.Sprintf("%v -> %v", persistentId, k)
			redisValue := config.GetRedis().Get(ctx, redisKey).Val()
			if redisValue == types.Written {
				value, ok = node.Attributes.RemoteHash, true
			}
			if redisValue == types.Deleted {
				value, ok = "", true
			}
			if !ok && node.Attributes.DestinationFile.Hash != "" {
				jobNodes[k] = node
				value = "?"
			}
			node.Attributes.DestinationFile.Hash = value
			node.Attributes.DestinationFile.HashType = node.Attributes.RemoteHashType
		}
		res[k] = node
	}
	if len(jobNodes) > 0 && addJobs {
		AddJob(ctx,
			Job{
				DataverseKey:  dataverseKey,
				User:          user,
				PersistentId:  persistentId,
				WritableNodes: jobNodes,
				Plugin:        "hash-only",
			},
		)
	}
	return res, len(jobNodes) > 0
}

func doRehash(ctx context.Context, dataverseKey, user, persistentId string, nodes map[string]tree.Node, in Job) (out Job, err error) {
	err = Destination.CheckPermission(ctx, dataverseKey, user, persistentId)
	if err != nil {
		return
	}
	knownHashes := getKnownHashes(ctx, persistentId)
	defer func() {
		storeKnownHashes(ctx, persistentId, knownHashes)
	}()
	out = in
	i := 0
	total := len(nodes)
	for k, node := range nodes {
		err = calculateHash(ctx, dataverseKey, user, persistentId, node, knownHashes)
		if err != nil {
			return
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(ctx, persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
			logging.Logger.Printf("%v: processed %v/%v\n", persistentId, i, total)
		}
		delete(out.WritableNodes, k)
	}
	return
}

func getKnownHashes(ctx context.Context, persistentId string) map[string]calculatedHashes {
	shortContext, cancel := context.WithTimeout(ctx, redisCtxDuration)
	defer cancel()
	res := map[string]calculatedHashes{}
	cache := config.GetRedis().Get(shortContext, "hashes: "+persistentId)
	err := json.Unmarshal([]byte(cache.Val()), &res)
	if err != nil {
		return map[string]calculatedHashes{}
	}
	return res
}

func storeKnownHashes(ctx context.Context, persistentId string, knownHashes map[string]calculatedHashes) {
	shortContext, cancel := context.WithTimeout(ctx, redisCtxDuration)
	defer cancel()
	knownHashesJson, err := json.Marshal(knownHashes)
	if err != nil {
		logging.Logger.Println("marshalling hashes failed")
		return
	}
	config.GetRedis().Set(shortContext, "hashes: "+persistentId, string(knownHashesJson), 0)
}

func invalidateKnownHashes(ctx context.Context, persistentId string) {
	shortContext, cancel := context.WithTimeout(ctx, redisCtxDuration)
	defer cancel()
	config.GetRedis().Del(shortContext, "hashes: "+persistentId)
}

func calculateHash(ctx context.Context, dataverseKey, user, persistentId string, node tree.Node, knownHashes map[string]calculatedHashes) error {
	hashType := node.Attributes.RemoteHashType
	known, ok := knownHashes[node.Id]
	if ok && known.LocalHashType == node.Attributes.DestinationFile.HashType && known.LocalHashValue == node.Attributes.DestinationFile.Hash {
		_, ok2 := known.RemoteHashes[hashType]
		if ok2 {
			return nil
		}
	} else {
		known = calculatedHashes{
			LocalHashType:  node.Attributes.DestinationFile.HashType,
			LocalHashValue: node.Attributes.DestinationFile.Hash,
			RemoteHashes:   map[string]string{},
		}
	}
	h, err := doHash(ctx, dataverseKey, user, persistentId, node)
	if err != nil {
		return fmt.Errorf("failed to hash local file %v: %v", node.Attributes.DestinationFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return nil
}

func CheckKnownHashes(ctx context.Context, persistentId string, mapped map[string]tree.Node) {
	knownHashes := getKnownHashes(ctx, persistentId)
	for k, v := range mapped {
		if knownHashes[k].LocalHashValue == "" {
			continue
		}
		invalid := knownHashes[k].LocalHashValue != v.Attributes.DestinationFile.Hash || knownHashes[k].LocalHashType != v.Attributes.DestinationFile.HashType
		if invalid {
			invalidateKnownHashes(ctx, persistentId)
			break
		}
	}
}
