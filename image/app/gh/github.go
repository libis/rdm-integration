package gh

import (
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

type CompareRequest struct {
	GhToken      string `json:"ghToken"`
	GhUser       string `json:"ghUser"`
	Repo         string `json:"repo"`
	Hash         string `json:"hash"`
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
}

type StoreRequest struct {
	GhToken       string      `json:"ghToken"`
	GhUser        string      `json:"ghUser"`
	Repo          string      `json:"repo"`
	PersistentId  string      `json:"persistentId"`
	DataverseKey  string      `json:"dataverseKey"`
	SelectedNodes []tree.Node `json:"selectedNodes"`
}

//TODO: run in a goroutine and write in cache + polling from cache
func GithubCompare(w http.ResponseWriter, r *http.Request) {
	//process request
	ctx := r.Context()
	req := CompareRequest{}
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
	res := utils.Compare(nm, req.PersistentId, req.DataverseKey)
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
		isFile := e.GetType() == "blob"
		parentId := ""
		ancestors := strings.Split(path, "/")
		fileName := path
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		node := tree.Node{
			Id:   path,
			Name: fileName,
			Path: parentId,
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

	streams := map[string]map[string]interface{}{}
	selected := map[string]tree.Node{}
	for _, v := range req.SelectedNodes {
		if v.Action == tree.Copy || v.Action == tree.Update {
			streams[v.Id] = map[string]interface{}{"sha": v.Attributes.RemoteHash}
		}
		selected[v.Id] = v
	}

	err = utils.AddJob(utils.Job{
		DataverseKey:  req.DataverseKey,
		PersistentId:  req.PersistentId,
		WritableNodes: selected,
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
	res := common.StoreResult{Status: "OK"}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}
