package g

// A struct that wraps an String for hashing.
type shash struct{ str String }

// Hash returns a shash struct wrapping the given String.
func (s String) Hash() shash { return shash{s} }

// MD5 computes the MD5 hash of the wrapped String and returns the hash as an String.
func (sh shash) MD5() String { return sh.str.Bytes().Hash().MD5().String() }

// SHA1 computes the SHA1 hash of the wrapped String and returns the hash as an String.
func (sh shash) SHA1() String { return sh.str.Bytes().Hash().SHA1().String() }

// SHA256 computes the SHA256 hash of the wrapped String and returns the hash as an String.
func (sh shash) SHA256() String { return sh.str.Bytes().Hash().SHA256().String() }

// SHA512 computes the SHA512 hash of the wrapped String and returns the hash as an String.
func (sh shash) SHA512() String { return sh.str.Bytes().Hash().SHA512().String() }
