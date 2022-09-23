package utils

import (
	"fmt"
	"integration/app/tree"
)

func localRehashToMatchRemoteHashType(doi string, nodes map[string]tree.Node) error {
	for k, node := range nodes {
		if node.Attributes.LocalHash != "" && node.Attributes.RemoteHashType != "" && node.Attributes.Metadata.DataFile.Checksum.Type != node.Attributes.RemoteHashType {
			h, err := doHash(doi, node)
			if err != nil {
				return fmt.Errorf("failed to hash local file %v: %v", node.Attributes.Metadata.DataFile.StorageIdentifier, err)
			}
			node.Attributes.LocalHash = fmt.Sprintf("%x", h)
			nodes[k] = node
		}
	}
	return nil
}
