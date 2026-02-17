// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"integration/app/tree"
)

const (
	New      = 0
	Updating = 1
	Finished = 2
)

type CompareResponse struct {
	Id          string      `json:"id"`
	Status      int         `json:"status"`
	Data        []tree.Node `json:"data"`
	Url         string      `json:"url"`
	MaxFileSize int64       `json:"maxFileSize,omitempty"`
	// RejectedSize: files excluded because they exceed the maximum file size
	RejectedSize []string `json:"rejectedSize,omitempty"`
	// RejectedName: files excluded because their name or path contains unsupported characters
	RejectedName []string `json:"rejectedName,omitempty"`
	// AllowedFileNamePattern: regex pattern describing allowed file name characters
	AllowedFileNamePattern string `json:"allowedFileNamePattern,omitempty"`
	// AllowedFolderPathPattern: regex pattern describing allowed folder path characters
	AllowedFolderPathPattern string `json:"allowedFolderPathPattern,omitempty"`
	// PreSelectedIds: file IDs preselected for download (e.g. from Globus downloadId)
	PreSelectedIds []int64 `json:"preSelectedIds,omitempty"`
}

func MergeNodeMaps(to, from map[string]tree.Node) map[string]tree.Node {
	res := map[string]tree.Node{}
	for k, v := range to {
		res[k] = v
	}
	for k, v := range from {
		if !v.Attributes.IsFile {
			continue
		}
		node, ok := to[k]
		if !ok {
			node = v
		}
		if node.Attributes.IsFile {
			node.Attributes.RemoteHash = v.Attributes.RemoteHash
			node.Attributes.RemoteHashType = v.Attributes.RemoteHashType
			node.Attributes.URL = v.Attributes.URL
		}
		res[k] = node
	}
	return res
}

func Compare(ctx context.Context, in map[string]tree.Node, pid, dataverseKey, user string, addJobs bool) CompareResponse {
	in, jobNeeded := localRehashToMatchRemoteHashType(ctx, dataverseKey, user, pid, in, addJobs)
	data := []tree.Node{}
	empty := false
	for _, v := range in {
		if !v.Attributes.IsFile {
			continue
		}
		if v.Attributes.RemoteHash != "" {
			switch {
			case v.Attributes.DestinationFile.Hash == "":
				v.Status = tree.New
			case v.Attributes.DestinationFile.Hash == "?":
				v.Status = tree.Unknown
			case v.Attributes.DestinationFile.Hash != v.Attributes.RemoteHash:
				v.Status = tree.Updated
			case v.Attributes.DestinationFile.Hash == v.Attributes.RemoteHash:
				v.Status = tree.Equal
			}
		} else {
			v.Status = tree.Deleted
		}
		data = append(data, v)
		empty = empty || v.Attributes.DestinationFile.Hash != ""
	}
	status := Finished
	if jobNeeded || IsLocked(ctx, pid) {
		status = Updating
	} else if empty {
		status = New
	}
	return CompareResponse{
		Id:     pid,
		Status: status,
		Data:   data,
		Url:    Destination.GetRepoUrl(pid, false),
	}
}
