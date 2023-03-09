// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"encoding/binary"
)

type QuickXorHash struct {
	fileSize         int64
	hash             []byte
	startBitPosition int
}

const l = 20
const shift = 11

func (h *QuickXorHash) Write(p []byte) (n int, err error) {
	h.xor(p)
	h.fileSize = h.fileSize + int64(len(p))
	return len(p), nil
}

func (h *QuickXorHash) xor(p []byte) {
	if len(h.hash) == 0 {
		h.hash = make([]byte, l)
	}
	bitLength := l * 8
	for i := 0; i < len(p); i++ {
		index := h.startBitPosition / 8
		next := (index + 1) % l
		nbBitsToShift := h.startBitPosition % 8
		shifted := int16(p[i]) << nbBitsToShift
		h.hash[index] = h.hash[index] ^ byte(shifted)
		h.hash[next] = h.hash[next] ^ byte(shifted>>8)
		h.startBitPosition = (h.startBitPosition + shift) % bitLength
	}
}

func (h *QuickXorHash) Sum(b []byte) []byte {
	res := make([]byte, l)
	copy(res, h.hash)
	sizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBytes, uint64(h.fileSize))
	for i := 0; i < 8; i++ {
		res[l-8+i] = res[l-8+i] ^ sizeBytes[i]
	}
	return append(b, res...)
}

func (h *QuickXorHash) Reset() {
	*h = QuickXorHash{}
}

func (h *QuickXorHash) Size() int {
	return l
}

func (h *QuickXorHash) BlockSize() int {
	return 64
}
