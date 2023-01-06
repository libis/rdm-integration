package ir

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"integration/app/common"
	"integration/app/tree"
	"integration/app/utils"
	"integration/app/utils/irods/client"
	"io"
	"net/http"
	"strings"

	"github.com/cyverse/go-irodsclient/fs"
)

type CompareRequest struct {
	User         string `json:"user"`
	Password     string `json:"password"`
	Server       string `json:"server"`
	Zone         string `json:"zone"`
	Folder       string `json:"folder"`
	PersistentId string `json:"persistentId"`
	DataverseKey string `json:"dataverseKey"`
}

func IrodsCompare(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisReady() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - cache not ready"))
		return
	}
	//process request
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
	key := fmt.Sprintf("cached compare response (%v): %v", utils.GitHash, req.PersistentId)
	go doIrodsCompare(req, key)
	res := common.Key{Key: key}
	b, err = json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("500 - %v", err)))
		return
	}
	w.Write(b)
}

func doIrodsCompare(req CompareRequest, key string) {
	cachedRes := common.CachedResponse{
		Key: key,
	}
	//check permission
	err := utils.CheckPermission(req.DataverseKey, req.PersistentId)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	//query dataverse
	nm, err := utils.GetNodeMap(req.PersistentId, req.DataverseKey)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	//query irods
	cl, err := client.NewIrodsClient(req.Server, req.Zone, req.User, req.Password)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}
	defer cl.Close()
	entries, err := cl.FileSystem.List(req.Folder)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}
	irodsNm, err := toNodeMap(cl, req.Folder, entries)
	utils.MergeNodeMaps(nm, irodsNm)

	//compare and write response
	res := utils.Compare(nm, req.PersistentId, req.DataverseKey)
	if err != nil {
		cachedRes.ErrorMessage = err.Error()
		common.CacheResponse(cachedRes)
		return
	}

	cachedRes.Response = res
	common.CacheResponse(cachedRes)
}

func toNodeMap(cl *client.IrodsClient, folder string, entries []*fs.Entry) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	dirs := []string{}
	for _, e := range entries {
		path := e.Path[len(folder)+1:]
		isFile := e.Type == "file"
		if !isFile {
			if e.Type == "directory" {
				dirs = append(dirs, e.Path)
			}
			continue
		}
		parentId := ""
		ancestors := strings.Split(path, "/")
		fileName := path
		if len(ancestors) > 1 {
			parentId = strings.Join(ancestors[:len(ancestors)-1], "/")
			fileName = ancestors[len(ancestors)-1]
		}
		hash := e.CheckSum
		hashType := utils.Md5
		if hash == "" {
			h := make([]byte, 8)
			binary.LittleEndian.PutUint64(h, uint64(e.Size))
			hash = fmt.Sprintf("%x", h)
			hashType = utils.FileSize
		}
		node := tree.Node{
			Id:   path,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     hash,
				RemoteHashType: hashType,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
						ContentType: "application/octet-stream",
						Filesize:    int(e.Size),
						Checksum: tree.Checksum{
							Type:  hashType,
							Value: hash,
						},
					},
				},
			},
		}
		res[path] = node
	}
	for _, d := range dirs {
		subEntries, err := cl.FileSystem.List(d)
		if err != nil {
			return nil, err
		}
		irodsNm, err := toNodeMap(cl, folder, subEntries)
		for k, v := range irodsNm {
			res[k] = v
		}
	}
	return res, nil
}
