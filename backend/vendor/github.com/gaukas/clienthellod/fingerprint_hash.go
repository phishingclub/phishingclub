package clienthellod

import (
	"encoding/binary"
	"hash"
)

func updateArr(h hash.Hash, arr []byte) {
	binary.Write(h, binary.BigEndian, uint32(len(arr)))
	h.Write(arr)
}

func updateU32(h hash.Hash, i uint32) {
	binary.Write(h, binary.BigEndian, i)
}

func updateU64(h hash.Hash, i uint64) {
	binary.Write(h, binary.BigEndian, i)
}
