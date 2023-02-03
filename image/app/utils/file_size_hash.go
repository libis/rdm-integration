// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package utils

import "encoding/binary"

type FileSizeHash struct {
	FileSize int
}

// Write (via the embedded io.Writer interface) adds more data to the running hash.
// It never returns an error.
func (h *FileSizeHash) Write(p []byte) (n int, err error) {
	h.FileSize = h.FileSize + len(p)
	return len(p), nil
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (h *FileSizeHash) Sum(b []byte) []byte {
	res := make([]byte, 8)
	binary.LittleEndian.PutUint64(res, uint64(h.FileSize))
	return res
}

// Reset resets the Hash to its initial state.
func (h *FileSizeHash) Reset() {}

// Size returns the number of bytes Sum will return.
func (h *FileSizeHash) Size() int {
	return 8
}

// BlockSize returns the hash's underlying block size.
// The Write method must be able to accept any amount
// of data, but it may operate more efficiently if all writes
// are a multiple of the block size.
func (h *FileSizeHash) BlockSize() int {
	return 256
}
