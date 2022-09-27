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

func localRehashToMatchRemoteHashType(persistentId string, nodes map[string]tree.Node) error {
	knownHashes := getKnownHashes(persistentId)
	store := false
	for k, node := range nodes {
		if node.Attributes.LocalHash != "" && node.Attributes.RemoteHashType != "" && node.Attributes.Metadata.DataFile.Checksum.Type != node.Attributes.RemoteHashType {
			recalculated, err := calculateHash(persistentId, node, knownHashes)
			if err != nil {
				return err
			}
			store = store || recalculated
			node.Attributes.LocalHash = knownHashes[node.Id].RemoteHashes[node.Attributes.RemoteHashType]
			nodes[k] = node
		}
	}
	if store {
		storeKnownHashes(persistentId, knownHashes)
	}
	return nil
}

func getKnownHashes(persistentId string) map[string]calculatedHashes {
	res := map[string]calculatedHashes{}
	cache := rdb.Get(context.Background(), "hashes: "+persistentId)
	json.Unmarshal([]byte(cache.Val()), &res)
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

func calculateHash(persistentId string, node tree.Node, knownHashes map[string]calculatedHashes) (bool, error) {
	hashType := node.Attributes.RemoteHashType
	known, ok := knownHashes[node.Id]
	if ok && known.LocalHashType == node.Attributes.Metadata.DataFile.Checksum.Type && known.LocalHashValue == node.Attributes.Metadata.DataFile.Checksum.Value {
		_, ok2 := known.RemoteHashes[hashType]
		if ok2 {
			return false, nil
		}
	} else {
		known = calculatedHashes{
			LocalHashType:  node.Attributes.Metadata.DataFile.Checksum.Type,
			LocalHashValue: node.Attributes.Metadata.DataFile.Checksum.Value,
			RemoteHashes:   map[string]string{},
		}
	}
	h, err := doHash(persistentId, node)
	if err != nil {
		return false, fmt.Errorf("failed to hash local file %v: %v", node.Attributes.Metadata.DataFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return true, nil
}
