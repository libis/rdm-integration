// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package local

import (
	"context"
	"crypto/md5"
	"fmt"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
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

func Query(_ context.Context, req types.CompareRequest, dvNodes map[string]tree.Node) (map[string]tree.Node, error) {
	path := strings.TrimSuffix(req.Url, string(os.PathSeparator))
	entries, err := list(path, path, dvNodes)
	if err != nil {
		return nil, err
	}
	dirs, nodes, err := toNodeMap(entries)
	if err != nil {
		return nil, err
	}
	for len(dirs) != 0 {
		moreDirs := []string{}
		for _, d := range dirs {
			subEntries, err := list(path, d, dvNodes)
			if err != nil {
				return nil, err
			}
			var nm map[string]tree.Node
			var subDirs []string
			subDirs, nm, err = toNodeMap(subEntries)
			if err != nil {
				return nil, err
			}
			for k, v := range nm {
				nodes[k] = v
			}
			moreDirs = append(moreDirs, subDirs...)
		}
		dirs = moreDirs
	}
	return nodes, nil
}

func toNodeMap(entries []Entry) ([]string, map[string]tree.Node, error) {
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
				IsFile:         isFile,
				RemoteHash:     e.CheckSum,
				RemoteHashType: types.Md5,
				RemoteFileSize: e.Size,
			},
		}
		res[e.Id] = node
	}
	return dirs, res, nil
}

func list(root, folder string, dvNodes map[string]tree.Node) ([]Entry, error) {
	files, err := os.ReadDir(folder)
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
		var size int64
		if !idDir {
			info, err := v.Info()
			if err == nil {
				size = info.Size()
			}
			id = fileName
			if len(folder) > len(root) {
				ancestors := strings.Split(folder[len(root)+1:], string(os.PathSeparator))
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
