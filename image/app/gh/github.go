package gh

import (
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

type TreeRequest struct {
	GhToken      string `json:"ghToken"`
	GhUser       string `json:"ghUser"`
	Repo         string `json:"repo"`
	Hash         string `json:"hash"`
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
}

type StoreRequest struct {
	GhToken       string       `json:"ghToken"`
	GhUser        string       `json:"ghUser"`
	Repo          string       `json:"repo"`
	PersistentId  string       `json:"persistentId"`
	DataverseKey  string       `json:"dataverseKey"`
	SelectedNodes []*tree.Node `json:"selectedNodes"`
	OriginalRoot  tree.Node    `json:"originalRoot"`
	ToUpdate      []string     `json:"toUpdate"`
	ToDelete      []string     `json:"toDelete"`
	ToAdd         []string     `json:"toAdd"`
}

type WritableNodesResponse struct {
	ToUpdate []string `json:"toUpdate"`
	ToDelete []string `json:"toDelete"`
	ToAdd    []string `json:"toAdd"`
}

func GithubTree(w http.ResponseWriter, r *http.Request) {
	//process request
	ctx := r.Context()
	req := TreeRequest{}
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	//query dataverse
	nm, err := utils.GetNodeMap(req.PersistentId, req.DataverseKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	//query github
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: req.GhToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	tr, _, err := client.Git.GetTree(ctx, req.GhUser, req.Repo, req.Hash, true)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	utils.MergeNodeMaps(nm, toNodeMap(tr))

	//compare and write response
	res, err := utils.GetWiredRootNode(req.PersistentId, nm)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func toNodeMap(tr *github.Tree) map[string]tree.Node {
	res := map[string]tree.Node{}
	for _, e := range tr.Entries {
		path := e.GetPath()
		isFile := e.GetType() != "folder"
		parentId := ""
		ancestors := strings.Split(path, "/")
		fileName := path
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		node := tree.Node{
			Id:   path,
			Html: fileName,
			Attributes: tree.Attributes{
				URL:            e.GetURL(),
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     e.GetSHA(),
				RemoteHashType: utils.GitHash,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    e.GetSize(),
						Checksum: tree.Checksum{
							Type:  utils.GitHash,
							Value: e.GetSHA(),
						},
					},
				},
			},
		}
		res[path] = node
	}
	return res
}

func GithubStore(w http.ResponseWriter, r *http.Request) {
	req := StoreRequest{}
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	writableNodes := utils.ToWritableNodes(req.SelectedNodes, req.OriginalRoot)
	streams := map[string]map[string]interface{}{}
	selected := map[string]bool{}
	selectedSlice := append(req.ToDelete, req.ToUpdate...)
	selectedSlice = append(selectedSlice, req.ToAdd...)
	for _, s := range selectedSlice {
		selected[s] = true
	}
	for k, v := range writableNodes {
		if v.Checked && selected[v.Id] {
			streams[k] = map[string]interface{}{"sha": v.Attributes.RemoteHash}
		}
		if !selected[v.Id] {
			delete(writableNodes, v.Id)
		}
	}

	err = utils.AddJob(utils.Job{
		DataverseKey:  req.DataverseKey,
		PersistentId:  req.PersistentId,
		WritableNodes: writableNodes,
		StreamType:    "github",
		Streams:       streams,
		StreamParams: map[string]string{
			"user":  req.GhUser,
			"repo":  req.Repo,
			"token": req.GhToken,
		},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
}

func GetWritable(w http.ResponseWriter, r *http.Request) {
	req := StoreRequest{}
	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	err = json.Unmarshal(b, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}

	writableNodes := utils.ToWritableNodes(req.SelectedNodes, req.OriginalRoot)
	toUpdate := []string{}
	toDelete := []string{}
	toAdd := []string{}
	for _, v := range writableNodes {
		if v.Checked {
			if v.Attributes.LocalHash == "" {
				toAdd = append(toAdd, v.Id)
			} else {
				toUpdate = append(toUpdate, v.Id)
			}
		} else {
			toDelete = append(toDelete, v.Id)
		}
	}
	
	sort.Strings(toUpdate)
	sort.Strings(toDelete)
	sort.Strings(toAdd)

	res := WritableNodesResponse{
		ToUpdate: toUpdate,
		ToDelete: toDelete,
		ToAdd:    toAdd,
	}

	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
