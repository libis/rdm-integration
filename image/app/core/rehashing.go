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
	"strings"
)

type calculatedHashes struct {
	LocalHashType          string
	LocalHashValue         string
	LocalStorageIdentifier string `json:",omitempty"`
	RemoteHashes           map[string]string
}

func localRehashToMatchRemoteHashType(ctx context.Context, dataverseKey, user, persistentId string, nodes map[string]tree.Node, addJobs bool) (map[string]tree.Node, bool) {
	knownHashes := getKnownHashes(ctx, persistentId)
	jobNodes := map[string]tree.Node{}
	res := map[string]tree.Node{}
	jobNeeded := false
	for k, node := range nodes {
		if node.Attributes.RemoteHashType != "" {
			redisKey := fmt.Sprintf("%v -> %v", persistentId, k)
			redisValue := config.GetRedis().Get(ctx, redisKey).Val()
			if redisValue == types.Written {
				node.Attributes.DestinationFile.HashType = node.Attributes.RemoteHashType
			}
			value, needsRehashJob := resolveDestinationHash(node, knownHashes[node.Id], redisValue)
			// A fast transfer can become visible before its record is saved.
			// Revisit a cached unknown when the matching record arrives.
			if strings.EqualFold(node.Attributes.RemoteHashType, types.LastModified) && value == fmt.Sprintf("%x", "unknown") {
				if _, ok := globusTransferLastModified(ctx, persistentId, node); ok {
					value, needsRehashJob = "?", true
				}
			}
			jobNeeded = jobNeeded || needsRehashJob
			// An echoed "?" carries no destination hash to key the cache on.
			if needsRehashJob && node.Attributes.DestinationFile.Hash != "?" {
				jobNodes[k] = node
			}
			node.Attributes.DestinationFile.Hash = value
		}
		res[k] = node
	}
	if len(jobNodes) > 0 && addJobs {
		err := AddJob(ctx,
			Job{
				DataverseKey:  dataverseKey,
				User:          user,
				PersistentId:  persistentId,
				WritableNodes: jobNodes,
				Plugin:        "hash-only",
			},
		)
		if err != nil {
			logging.Logger.Println("adding rehashing job failed: " + err.Error())
		}
	}
	return res, jobNeeded
}

// resolveDestinationHash determines the destination-side hash (in the remote
// hash type) used for the equality comparison, and whether a rehashing job is
// needed to compute it. A cached rehash is only trusted when it provably
// describes the current destination content: the cache entry must record the
// same destination hash as the live listing. This covers both a file removed
// outside the integration (empty listing hash: it would otherwise be shown as
// present and equal — impossible to re-upload or delete) and a file replaced
// outside the integration (different listing hash: the cached rehash is for
// the old content). A fresh "written" marker still takes precedence, so files
// uploaded moments ago are correctly shown while the destination listing
// catches up.
func resolveDestinationHash(node tree.Node, known calculatedHashes, redisValue string) (value string, needsRehashJob bool) {
	value, ok := "", false
	if cacheMatchesDestination(node, known) {
		value, ok = known.RemoteHashes[node.Attributes.RemoteHashType]
	}
	if node.Attributes.DestinationFile.Hash != "" && node.Attributes.RemoteHashType == node.Attributes.DestinationFile.HashType {
		value, ok = node.Attributes.DestinationFile.Hash, true
	}
	if redisValue == types.Written {
		value, ok = node.Attributes.RemoteHash, true
	}
	if redisValue == types.Deleted {
		value, ok = "", true
	}
	if !ok && node.Attributes.DestinationFile.Hash != "" {
		return "?", true
	}
	return value, false
}

func cacheMatchesDestination(node tree.Node, known calculatedHashes) bool {
	destination := node.Attributes.DestinationFile
	if known.LocalHashType == "" || known.LocalHashType != destination.HashType || known.LocalHashValue != destination.Hash {
		return false
	}
	if !strings.EqualFold(node.Attributes.RemoteHashType, types.LastModified) {
		return true
	}
	// A timestamp describes one storage object. In particular, Dataverse's
	// "Not available in Dataverse" checksum cannot distinguish replacements.
	// Old cache entries without this binding must be recalculated as well.
	if sameStorageObject(known.LocalStorageIdentifier, destination.StorageIdentifier) {
		return true
	}
	// A listing with no storage identifier can only resolve to unknown. Keep
	// that negative result cacheable instead of repeatedly queuing jobs.
	return known.LocalStorageIdentifier == "" && destination.StorageIdentifier == "" &&
		known.RemoteHashes[node.Attributes.RemoteHashType] == fmt.Sprintf("%x", "unknown")
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
			if isNotFound(err) {
				logging.Logger.Printf("%v: skipping hash for %v: file not found in storage, removing from job\n", persistentId, node.Attributes.DestinationFile.StorageIdentifier)
				delete(out.WritableNodes, k)
				err = nil
				continue
			}
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
	known := knownHashes[node.Id]
	if !cacheMatchesDestination(node, known) {
		known = calculatedHashes{
			LocalHashType:          node.Attributes.DestinationFile.HashType,
			LocalHashValue:         node.Attributes.DestinationFile.Hash,
			LocalStorageIdentifier: node.Attributes.DestinationFile.StorageIdentifier,
			RemoteHashes:           map[string]string{},
		}
	}
	// A transfer we started wins over anything cached: re-uploading identical
	// content yields the same destination checksum, so the cache alone would
	// keep an earlier "unknown" for ever.
	if strings.EqualFold(hashType, types.LastModified) {
		if ts, ok := globusTransferLastModified(ctx, persistentId, node); ok {
			known.RemoteHashes[hashType] = ts
			knownHashes[node.Id] = known
			return nil
		}
	}
	if _, ok := known.RemoteHashes[hashType]; ok {
		return nil
	}
	h, err := doHash(ctx, dataverseKey, user, persistentId, node)
	if err != nil {
		return fmt.Errorf("failed to hash local file %v: %w", node.Attributes.DestinationFile.StorageIdentifier, err)
	}
	known.RemoteHashes[hashType] = fmt.Sprintf("%x", h)
	knownHashes[node.Id] = known
	return nil
}

// globusTransferLastModified returns the source timestamp recorded when this
// integration transferred the object that now backs the node. The record is
// trusted only for that object: a file that reached Dataverse any other way,
// or replaced the transferred one, gets no timestamp. The record is kept, so
// a wiped hash cache can be rebuilt as long as the object is the same.
func globusTransferLastModified(ctx context.Context, persistentId string, node tree.Node) (string, bool) {
	raw := config.GetRedis().Get(ctx, types.GlobusTransferKey(persistentId, node.Id)).Val()
	if raw == "" {
		return "", false
	}
	t := types.GlobusTransfer{}
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return "", false
	}
	if t.LastModified == "" || !sameStorageObject(t.StorageIdentifier, node.Attributes.DestinationFile.StorageIdentifier) {
		return "", false
	}
	return t.LastModified, true
}

// sameStorageObject compares storage identifiers, allowing a legacy listing
// to omit the prefix. When both identifiers include a storage location, it
// must match too: equal basenames in different stores are different objects.
// Dataverse hands out "s3://bucket:id" for a Globus upload and lists the
// file as "s3://bucket:id" again; some stores use a slash before the id.
func sameStorageObject(a, b string) bool {
	ap, aid := storageObjectParts(a)
	bp, bid := storageObjectParts(b)
	return aid != "" && aid == bid && (ap == "" || bp == "" || ap == bp)
}

func storageObjectParts(storageIdentifier string) (string, string) {
	i := strings.LastIndexAny(storageIdentifier, ":/")
	if i < 0 {
		return "", storageIdentifier
	}
	return strings.TrimRight(storageIdentifier[:i], "/"), storageIdentifier[i+1:]
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
