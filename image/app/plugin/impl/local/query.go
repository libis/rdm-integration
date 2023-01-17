package local

import (
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"io/ioutil"
	"os"
)

type Entry struct {
	Path     string
	ParentId string
	FileName string
	IsDir    bool
	CheckSum string
	Size     int64
}

func Query(req types.CompareRequest) (map[string]tree.Node, error) {
	entries, err := list(req.Url, req.Url)
	if err != nil {
		return nil, err
	}
	return toNodeMap(req.Url, req.Url, entries)
}

func toNodeMap(root, folder string, entries []Entry) (map[string]tree.Node, error) {
	res := map[string]tree.Node{}
	dirs := []string{}
	for _, e := range entries {
		path := e.Path[len(folder)+1:]
		isFile := !e.IsDir
		if !isFile {
			dirs = append(dirs, e.Path)
			continue
		}
		parentId := e.ParentId
		fileName := e.FileName
		node := tree.Node{
			Id:   path,
			Name: fileName,
			Path: parentId,
			Attributes: tree.Attributes{
				ParentId:       parentId,
				IsFile:         isFile,
				RemoteHash:     e.CheckSum,
				RemoteHashType: types.Md5,
				Metadata: tree.Metadata{
					Label:          fileName,
					DirectoryLabel: parentId,
					DataFile: tree.DataFile{
						Filename:    fileName,
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
		res[path] = node
	}
	for _, d := range dirs {
		subEntries, err := list(root, d)
		if err != nil {
			return nil, err
		}
		irodsNm, err := toNodeMap(root, folder, subEntries)
		for k, v := range irodsNm {
			res[k] = v
		}
	}
	return res, nil
}

func list(root, folder string) ([]Entry, error) {
	files, err := ioutil.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	res := []Entry{}
	parent := folder[len(root)+1:]
	for _, v := range files {
		path := folder + string(os.PathSeparator) + v.Name()
		checkSum := ""
		if !v.IsDir() {
			checkSum, err = hash(path)
			if err != nil {
				return nil, err
			}
		}
		res = append(res, Entry{
			Path:     path,
			ParentId: parent,
			FileName: v.Name(),
			IsDir:    v.IsDir(),
			CheckSum: checkSum,
			Size:     v.Size(),
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
