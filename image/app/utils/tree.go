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

func Compare(in map[string]tree.Node, pid string) (CompareResponse, error) {
	//TODO: rehashing in a job
	err := localRehashToMatchRemoteHashType(pid, in)
	if err != nil {
		return CompareResponse{}, err
	}
	data := []tree.Node{}
	for _, v := range in {
		if !v.Attributes.IsFile {
			continue;
		}
		v.Status = tree.Deleted
		if v.Attributes.RemoteHash != "" {
			switch {
			case v.Attributes.LocalHash == "":
				v.Status = tree.New
			case v.Attributes.LocalHash != v.Attributes.RemoteHash:
				v.Status = tree.Updated
			case v.Attributes.LocalHash == v.Attributes.RemoteHash:
				v.Status = tree.Equal
			}
		}
		v.Action = tree.Ignore
		data = append(data, v)
	}
	return CompareResponse{
		Id:     pid,
		Status: Finished,
		Data:   data,
	}, nil
}
