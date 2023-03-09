// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import "encoding/binary"

type FileSizeHash struct {
	FileSize int64
}

func (h *FileSizeHash) Write(p []byte) (n int, err error) {
	h.FileSize = h.FileSize + int64(len(p))
	return len(p), nil
}

func (h *FileSizeHash) Sum(b []byte) []byte {
	res := make([]byte, 8)
	binary.LittleEndian.PutUint64(res, uint64(h.FileSize))
	return append(b, res...)
}

func (h *FileSizeHash) Reset() {
	*h = FileSizeHash{}
}

func (h *FileSizeHash) Size() int {
	return 8
}

func (h *FileSizeHash) BlockSize() int {
	return 64
}
