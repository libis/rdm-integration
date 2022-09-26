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

func localRehashToMatchRemoteHashType(doi string, nodes map[string]tree.Node) error {
	knownHashes := getKnownHashes(doi)
	store := false
	for k, node := range nodes {
		if node.Attributes.LocalHash != "" && node.Attributes.RemoteHashType != "" && node.Attributes.Metadata.DataFile.Checksum.Type != node.Attributes.RemoteHashType {
			recalculated, err := calculateHash(doi, node, knownHashes)
			if err != nil {
				return err
			}
			store = store || recalculated
			node.Attributes.LocalHash = knownHashes[node.Id].RemoteHashes[node.Attributes.RemoteHashType]
			nodes[k] = node
		}
	}
	if store {
		storeKnownHashes(doi, knownHashes)
	}
	return nil
}

func getKnownHashes(doi string) map[string]calculatedHashes {
	res := map[string]calculatedHashes{}
	cache := rdb.Get(context.Background(), "hashes: "+doi)
	json.Unmarshal([]byte(cache.Val()), &res)
	return res
}

func storeKnownHashes(doi string, knownHashes map[string]calculatedHashes) {
	knownHashesJson, err := json.Marshal(knownHashes)
	if err != nil {
		logging.Logger.Println("marshalling hashes failed")
		return
	}
	res := rdb.Set(context.Background(), "hashes: "+doi, knownHashesJson, 0)
	logging.Logger.Println("hashes stored for", doi, len(knownHashes), res.Err())
}

func calculateHash(doi string, node tree.Node, knownHashes map[string]calculatedHashes) (bool, error) {
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
	h, err := doHash(doi, node)
	if err != nil {
		return false, fmt.Errorf("failed to hash local file %v: %v", node.Attributes.Metadata.DataFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return true, nil
}
