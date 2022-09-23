package utils

import (
	"fmt"
	"integration/app/tree"
	"sort"
	"strings"
)

func ToWritableNodes(selectedNodes []*tree.Node, originalRoot tree.Node) map[string]tree.Node {
	writableNodes := map[string]tree.Node{}
	toWritableNodes(tree.Node{Children: selectedNodes}, writableNodes, func(string) bool { return true })
	selected := map[string]bool{}
	for _, v := range selectedNodes {
		selected[v.Id] = true
	}
	toWritableNodes(originalRoot, writableNodes, func(s string) bool {
		return selected[s]
	})
	return writableNodes
}

func toWritableNodes(node tree.Node, m map[string]tree.Node, checked func(string) bool) {
	node.Checked = checked(node.Id)
	if !node.Checked && node.Attributes.LocalHash != "" {
		m[node.Id] = node
	}
	if node.Checked && node.Attributes.IsFile && node.Attributes.RemoteHash != "" && node.Attributes.LocalHash != node.Attributes.RemoteHash {
		m[node.Id] = node
	}
	for _, n := range node.Children {
		toWritableNodes(*n, m, checked)
	}
}

func MergeTrees(to, from map[string]tree.Node) {
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

func GetWiredRootNode(doi string, nodes map[string]tree.Node) (*tree.Node, error) {
	folders := map[string]bool{}
	for _, v := range nodes {
		folders[v.Attributes.ParentId] = true
	}
	for k := range folders {
		ancestors := strings.Split(k, "/")
		for i := range ancestors {
			parentId := ""
			if i > 0 {
				parentId = strings.Join(ancestors[:i], "/")
			}
			dir := strings.Join(ancestors[:i+1], "/")
			nodes[k] = tree.Node{
				Id:   dir,
				Html: dir,
				Attributes: tree.Attributes{
					ParentId: parentId,
				},
			}
		}
	}
	res := map[string]*tree.Node{}
	children := map[string][]*tree.Node{}
	for k, v := range nodes {
		node := v
		if v.Attributes.IsFile {
			if v.Attributes.RemoteHash == "" {
				node.Html = "<span style=\"color: red;\">" + v.Html + "</span>"
			} else if v.Attributes.LocalHash == "" {
				node.Html = "<span style=\"color: black;\">" + v.Html + "</span>"
			} else if v.Attributes.Metadata.DataFile.Checksum.Type != v.Attributes.RemoteHashType {
				h, err := doHash(doi, node)
				if err != nil {
					return nil, fmt.Errorf("failed to hash local file %v: %v", v.Attributes.Metadata.DataFile.StorageIdentifier, err)
				}
				node.Attributes.LocalHash = fmt.Sprintf("%x", h)
				if node.Attributes.LocalHash == node.Attributes.RemoteHash {
					node.Html = "<span style=\"color: green;\">" + v.Html + "</span>"
				} else {
					node.Html = "<span style=\"color: blue;\">" + v.Html + "</span>"
				}
			} else if v.Attributes.LocalHash == v.Attributes.RemoteHash {
				node.Html = "<span style=\"color: green;\">" + v.Html + "</span>"
			} else {
				node.Html = "<span style=\"color: blue;\">" + v.Html + "</span>"
			}
		}
		res[k] = &node
		if v.Id != "" {
			children[v.Attributes.ParentId] = append(children[v.Attributes.ParentId], &node)
		}
	}
	for k := range res {
		sort.Slice(children[k], func(i, j int) bool {
			return strings.ToLower(children[k][i].Id) < strings.ToLower(children[k][j].Id)
		})
		res[k].Children = children[k]
	}

	return res[""], nil
}
