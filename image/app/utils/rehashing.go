package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/tree"
)

type calculatedHashes struct {
	LocalHashType  string
	LocalHashValue string
	RemoteHashes   map[string]string
}

func localRehashToMatchRemoteHashType(dataverseKey, persistentId string, nodes map[string]tree.Node) bool {
	knownHashes := getKnownHashes(persistentId)
	jobNodes := map[string]tree.Node{}
	for k, node := range nodes {
		if node.Attributes.RemoteHashType != "" {
			value, ok := knownHashes[node.Id].RemoteHashes[node.Attributes.RemoteHashType]
			if !ok && node.Attributes.LocalHash != "" {
				jobNodes[k] = node
				value = "?"
			}
			node.Attributes.LocalHash = value
			nodes[k] = node
		}
	}
	if len(jobNodes) > 0 && dataverseKey != "" {
		AddJob(
			Job{
				DataverseKey:  dataverseKey,
				PersistentId:  persistentId,
				WritableNodes: jobNodes,
				StreamType:    "hash-only",
			},
		)
	}
	return len(jobNodes) > 0
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
		err = calculateHash(ctx, persistentId, node, knownHashes)
		if err != nil {
			return
		}
		i++
		if i%10 == 0 && i < total {
			storeKnownHashes(persistentId, knownHashes) //if we have many files to hash -> polling at the gui is happier to see some progress
		}
		delete(out.WritableNodes, k)
	}
	return
}

func getKnownHashes(persistentId string) map[string]calculatedHashes {
	res := map[string]calculatedHashes{}
	cache := rdb.Get(context.Background(), "hashes: "+persistentId)
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
	res := rdb.Set(context.Background(), "hashes: "+persistentId, knownHashesJson, 0)
	logging.Logger.Println("hashes stored for", persistentId, len(knownHashes), res.Err())
}

func invalidateKnownHashes(persistentId string) {
	rdb.Del(context.Background(), "hashes: "+persistentId)
}

func calculateHash(ctx context.Context, persistentId string, node tree.Node, knownHashes map[string]calculatedHashes) error {
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
	h, err := doHash(ctx, persistentId, node)
	if err != nil {
		return fmt.Errorf("failed to hash local file %v: %v", node.Attributes.Metadata.DataFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return nil
}
