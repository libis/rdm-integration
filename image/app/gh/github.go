package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/tree"
	"integration/app/utils"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

type TreeRequest struct {
	GhToken      string `json:"ghToken"`
	GhUser       string `json:"ghUser"`
	Repo         string `json:"repo"`
	Hash         string `json:"hash"`
	Doi          string `json:"doi"`
	DataverseKey string `json:"dataverseKey"`
}

type StoreRequest struct {
	GhToken       string       `json:"ghToken"`
	GhUser        string       `json:"ghUser"`
	Repo          string       `json:"repo"`
	Doi           string       `json:"doi"`
	DataverseKey  string       `json:"dataverseKey"`
	SelectedNodes []*tree.Node `json:"selectedNodes"`
	OriginalRoot  tree.Node    `json:"originalRoot"`
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
	nm, err := utils.GetNodeMap(req.Doi, req.DataverseKey)
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
	addTree(nm, tr)

	//compare and write response
	res, err := utils.GetWiredRootNode(req.Doi, nm)
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

func addTree(nm map[string]tree.Node, tr *github.Tree) {
	for _, e := range tr.Entries {
		path := e.GetPath()
		node, ok := nm[path]
		isFile := e.GetType() != "folder"
		parentId := ""
		ancestors := strings.Split(path, "/")
		fileName := path
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		if !ok {
			node = tree.Node{
				Id:   path,
				Html: fileName,
				Attributes: tree.Attributes{
					URL:      e.GetURL(),
					ParentId: parentId,
					IsFile:   isFile,
					Metadata: tree.Metadata{
						Label:          fileName,
						DirectoryLabel: parentId,
						DataFile: tree.DataFile{
							Filename:    fileName,
							ContentType: "application/octet-stream",
							Checksum: tree.Checksum{
								Type:  utils.GitHash,
								Value: e.GetSHA(),
							},
						},
					},
				},
			}
		}
		if node.Attributes.IsFile {
			node.Attributes.RemoteHash = e.GetSHA()
			node.Attributes.RemoteHashType = utils.GitHash
		}
		nm[path] = node
	}
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
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: req.GhToken},
	)
	ctx := context.Background()
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	readers := map[string]utils.Stream{}
	writableNodes := utils.ToWritableNodes(req.SelectedNodes, req.OriginalRoot)
	for k, v := range writableNodes {
		sha := v.Attributes.RemoteHash
		var gitErr error
		readers[k] = utils.Stream{
			Open: func() io.Reader {
				var b2 []byte
				b2, _, gitErr = client.Git.GetBlobRaw(ctx, req.GhUser, req.Repo, sha)
				return bytes.NewReader(b2)
			},
			Close: func() error {
				return gitErr
			},
		}
	}
	utils.PersistNodeMap(req.DataverseKey, req.Doi, writableNodes, readers)
}
