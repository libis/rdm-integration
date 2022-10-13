package utils

import (
	"integration/app/tree"
)

const (
	New      = 0
	Updating = 1
	Finished = 2
)

type CompareResponse struct {
	Id     string      `json:"id"`
	Status int         `json:"status"`
	Data   []tree.Node `json:"data"`
}

func MergeNodeMaps(to, from map[string]tree.Node) {
	for k, v := range from {
		node, ok := to[k]
		if !ok {
			node = v
		}
		if node.Attributes.IsFile {
			node.Attributes.RemoteHash = v.Attributes.RemoteHash
			node.Attributes.RemoteHashType = v.Attributes.RemoteHashType
		}
		to[k] = node
	}
}

func Compare(in map[string]tree.Node, pid, dataverseKey string) CompareResponse {
	jobNeeded := localRehashToMatchRemoteHashType(dataverseKey, pid, in)
	data := []tree.Node{}
	empty := false
	for _, v := range in {
		if !v.Attributes.IsFile {
			continue
		}
		if v.Attributes.RemoteHash != "" {
			switch {
			case v.Attributes.LocalHash == "":
				v.Status = tree.New
			case v.Attributes.LocalHash == "?":
				v.Status = tree.Unknown
			case v.Attributes.LocalHash != v.Attributes.RemoteHash:
				v.Status = tree.Updated
			case v.Attributes.LocalHash == v.Attributes.RemoteHash:
				v.Status = tree.Equal
			}
		} else if v.Attributes.LocalHash == "" {
			v.Status = tree.Removed
		} else {
			v.Status = tree.Deleted
		}
		v.Action = tree.Ignore
		data = append(data, v)
		empty = empty || v.Attributes.LocalHash != ""
	}
	status := Finished
	if jobNeeded || IsLocked(pid) {
		status = Updating
	} else if empty {
		status = New
	}
	return CompareResponse{
		Id:     pid,
		Status: status,
		Data:   data,
	}
}
