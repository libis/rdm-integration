// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package local

import (
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

type Entry struct {
	Path     string
	ParentId string
	Id       string
	FileName string
	IsDir    bool
	CheckSum string
	Size     int64
}

func Query(req types.CompareRequest, dvNodes map[string]tree.Node) (map[string]tree.Node, error) {
	path := req.Url
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		path = path[:len(path)-1]
	}
	entries, err := list(path, path, dvNodes)
	if err != nil {
		return nil, err
	}
	return toNodeMap(path, path, entries, dvNodes)
}

func toNodeMap(root, folder string, entries []Entry, dvNodes map[string]tree.Node) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	dirs := []string{}
	for _, e := range entries {
		isFile := !e.IsDir
		if !isFile {
			dirs = append(dirs, e.Path)
			continue
		}

		node := tree.Node{
			Id:   e.Id,
			Name: e.FileName,
			Path: e.ParentId,
			Attributes: tree.Attributes{
				ParentId:       e.ParentId,
				IsFile:         isFile,
				RemoteHash:     e.CheckSum,
				RemoteHashType: types.Md5,
				Metadata: tree.Metadata{
					Label:          e.FileName,
					DirectoryLabel: e.ParentId,
					DataFile: tree.DataFile{
						Filename:    e.FileName,
						ContentType: "application/octet-stream",
						Filesize:    int(e.Size),
						Checksum: tree.Checksum{
							Type:  types.Md5,
							Value: e.CheckSum,
						},
					},
				},
			},
		}
		res[e.Id] = node
	}
	for _, d := range dirs {
		subEntries, err := list(root, d, dvNodes)
		if err != nil {
			return nil, err
		}
		irodsNm, err := toNodeMap(root, d, subEntries, dvNodes)
		for k, v := range irodsNm {
			res[k] = v
		}
	}
	return res, nil
}

func list(root, folder string, dvNodes map[string]tree.Node) ([]Entry, error) {
	files, err := ioutil.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	res := []Entry{}
	for _, v := range files {
		path := folder + string(os.PathSeparator) + v.Name()
		checkSum := types.NotNeeded
		parentId := ""
		id := ""
		fileName := v.Name()
		idDir := v.IsDir()
		size := v.Size()
		if !idDir {
			id = fileName
			ancestors := []string{}
			if len(folder) > len(root) {
				ancestors = strings.Split(folder[len(root)+1:], string(os.PathSeparator))
				parentId = strings.Join(ancestors, "/")
				id = parentId + "/" + fileName
			}
			if _, ok := dvNodes[id]; ok {
				checkSum, err = hash(path)
				if err != nil {
					return nil, err
				}
			}
		}
		res = append(res, Entry{
			Path:     path,
			ParentId: parentId,
			Id:       id,
			FileName: fileName,
			IsDir:    idDir,
			CheckSum: checkSum,
			Size:     size,
		})
	}
	return res, nil
}

func hash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := md5.New()
	io.Copy(hasher, f)
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
