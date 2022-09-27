package utils

import (
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

func GetWiredRootNode(persistentId string, nodes map[string]tree.Node) (*tree.Node, error) {
	err := localRehashToMatchRemoteHashType(persistentId, nodes)
	if err != nil {
		return nil, err
	}
	folders := getFolders(nodes)
	addFoldersToNodes(folders, nodes)
	res := map[string]*tree.Node{}
	children := map[string][]*tree.Node{}
	for k, v := range nodes {
		node := v
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
	for k := range res {
		res[k].Html = addColor(res[k])
	}

	return res[""], nil
}

func addColor(node *tree.Node) string {
	html := node.Html
	if node.Attributes.IsFile {
		if node.Attributes.RemoteHash == "" {
			html = "<span style=\"color: red;\">" + node.Html + "</span>"
		} else if node.Attributes.LocalHash == "" {
			html = "<span style=\"color: black;\">" + node.Html + "</span>"
		} else if node.Attributes.LocalHash == node.Attributes.RemoteHash {
			html = "<span style=\"color: green;\">" + node.Html + "</span>"
		} else {
			html = "<span style=\"color: blue;\">" + node.Html + "</span>"
		}
	} else {
		someGreen, allGreen := foldersOverlapping(node)
		if allGreen {
			html = "<span style=\"color: green;\">" + node.Html + "</span>"
		} else if someGreen {
			html = "<span style=\"color: blue;\">" + node.Html + "</span>"
		}
	}
	return html
}

func foldersOverlapping(node *tree.Node) (bool, bool) {
	if node.Attributes.SomeGreen != nil && node.Attributes.AllGreen != nil {
		return *node.Attributes.SomeGreen, *node.Attributes.AllGreen
	}
	all := len(node.Children) > 0
	some := false
	for i := range node.Children {
		child := node.Children[i]
		if child.Attributes.IsFile {
			all = all && child.Attributes.LocalHash == child.Attributes.RemoteHash
			some = some || child.Attributes.LocalHash == child.Attributes.RemoteHash
		} else {
			someGreen, allGreen := foldersOverlapping(child)
			all = all && allGreen
			some = some || someGreen
		}
	}
	node.Attributes.SomeGreen = &some
	node.Attributes.AllGreen = &all
	return some, all
}

func getFolders(nodes map[string]tree.Node) map[string]bool {
	folders := map[string]bool{}
	for _, v := range nodes {
		folders[v.Attributes.ParentId] = true
	}
	return folders
}

func addFoldersToNodes(folders map[string]bool, nodes map[string]tree.Node) {
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
}
