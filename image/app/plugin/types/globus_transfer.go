// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package types

import "fmt"

// GlobusTransfer records, for one file of a dataset, which destination object
// a Globus transfer started by this integration created and the source
// last_modified it carried. A last_modified hash cannot be recomputed from the
// stored file, so this record is the only way to show a transferred file as
// equal to its source. It is bound to the storage object: an object that got
// there any other way (a same-named upload through the UI, a replacement after
// a failed transfer) must not inherit the timestamp.
type GlobusTransfer struct {
	StorageIdentifier string `json:"storageIdentifier"`
	LastModified      string `json:"lastModified"`
}

func GlobusTransferKey(persistentId, nodeId string) string {
	return fmt.Sprintf("globus transfer %v -> %v", persistentId, nodeId)
}
