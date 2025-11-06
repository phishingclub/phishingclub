package g

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
)

// A struct that wraps an Bytes for hashing.
type bhash struct{ bytes Bytes }

// Hash returns a bhash struct wrapping the given Bytes.
func (bs Bytes) Hash() bhash { return bhash{bs} }

// MD5 computes the MD5 hash of the wrapped Bytes and returns the hash as an Bytes.
func (bh bhash) MD5() Bytes { return bytesHasher(md5.New(), bh.bytes) }

// SHA1 computes the SHA1 hash of the wrapped Bytes and returns the hash as an Bytes.
func (bh bhash) SHA1() Bytes { return bytesHasher(sha1.New(), bh.bytes) }

// SHA256 computes the SHA256 hash of the wrapped Bytes and returns the hash as an Bytes.
func (bh bhash) SHA256() Bytes { return bytesHasher(sha256.New(), bh.bytes) }

// SHA512 computes the SHA512 hash of the wrapped Bytes and returns the hash as an Bytes.
func (bh bhash) SHA512() Bytes { return bytesHasher(sha512.New(), bh.bytes) }

// bytesHasher a helper function that computes the hash of the given Bytes using the specified
// hash.Hash algorithm and returns the hash as an Bytes.
func bytesHasher(h hash.Hash, bs Bytes) Bytes {
	_, _ = h.Write(bs)
	sum := h.Sum(nil)
	out := make(Bytes, hex.EncodedLen(len(sum)))
	hex.Encode(out, sum)

	return out
}
