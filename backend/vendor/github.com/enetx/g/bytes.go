package g

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/enetx/g/cmp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

var (
	lower = cases.Lower(language.Und)
	upper = cases.Upper(language.Und)
	title = cases.Title(language.Und)
)

// NewBytes creates a new Bytes value.
func NewBytes(size ...Int) Bytes {
	var (
		length   Int
		capacity Int
	)

	switch {
	case len(size) > 1:
		length, capacity = size[0], size[1]
	case len(size) == 1:
		length, capacity = size[0], size[0]
	}

	return make([]byte, length, capacity)
}

// Transform applies a transformation function to the Bytes and returns the result.
func (bs Bytes) Transform(fn func(Bytes) Bytes) Bytes { return fn(bs) }

// Reverse reverses bytes for ASCII or invalid UTF-8 for valid UTF-8 it reverses by runes.
func (bs Bytes) Reverse() Bytes {
	n := len(bs)
	out := make(Bytes, n)
	ascii := true

	for _, b := range bs {
		if b >= utf8.RuneSelf {
			ascii = false
			break
		}
	}

	if ascii || !utf8.Valid(bs) {
		for i, b := range bs {
			out[n-1-i] = b
		}

		return out
	}

	w := 0

	for i := n; i > 0; {
		r, size := utf8.DecodeLastRune(bs[:i])
		w += utf8.EncodeRune(out[w:], r)
		i -= size
	}

	return out
}

// Replace replaces the first 'n' occurrences of 'oldB' with 'newB' in the Bytes.
func (bs Bytes) Replace(oldB, newB Bytes, n Int) Bytes { return bytes.Replace(bs, oldB, newB, n.Std()) }

// ReplaceAll replaces all occurrences of 'oldB' with 'newB' in the Bytes.
func (bs Bytes) ReplaceAll(oldB, newB Bytes) Bytes { return bytes.ReplaceAll(bs, oldB, newB) }

// Trim trims leading and trailing white space from the Bytes.
func (bs Bytes) Trim() Bytes { return bytes.TrimSpace(bs) }

// TrimStart removes leading white space from the Bytes.
func (bs Bytes) TrimStart() Bytes { return bytes.TrimLeftFunc(bs, unicode.IsSpace) }

// TrimEnd removes trailing white space from the Bytes.
func (bs Bytes) TrimEnd() Bytes { return bytes.TrimRightFunc(bs, unicode.IsSpace) }

// TrimSet trims the specified set of characters from both the beginning and end of the Bytes.
func (bs Bytes) TrimSet(cutset String) Bytes { return bytes.Trim(bs, cutset.Std()) }

// TrimStartSet removes the specified set of characters from the beginning of the Bytes.
func (bs Bytes) TrimStartSet(cutset String) Bytes { return bytes.TrimLeft(bs, cutset.Std()) }

// TrimEndSet removes the specified set of characters from the end of the Bytes.
func (bs Bytes) TrimEndSet(cutset String) Bytes { return bytes.TrimRight(bs, cutset.Std()) }

// intFromBytes parses up to 8 bytes into Int using the given byte order.
// For BE: uses the last 8 bytes if longer; pads on the left if shorter.
// For LE: uses the first 8 bytes if longer; pads on the right if shorter.
func intFromBytes(bs Bytes, order binary.ByteOrder) Int {
	var buf [8]byte

	switch order {
	case binary.BigEndian:
		if len(bs) > 8 {
			bs = bs[len(bs)-8:]
		}

		copy(buf[8-len(bs):], bs)

		if len(bs) > 0 && bs[0]&0x80 != 0 && len(bs) < 8 {
			for i := 0; i < 8-len(bs); i++ {
				buf[i] = 0xFF
			}
		}
	case binary.LittleEndian:
		if len(bs) > 8 {
			bs = bs[:8]
		}

		copy(buf[:len(bs)], bs)

		if len(bs) > 0 && bs[len(bs)-1]&0x80 != 0 && len(bs) < 8 {
			for i := len(bs); i < 8; i++ {
				buf[i] = 0xFF
			}
		}
	}

	return Int(int64(order.Uint64(buf[:])))
}

// IntBE interprets the Bytes as a signed 64-bit integer in BigEndian order.
// If the Bytes length is less than 8, it is padded with leading zeros.
// If the Bytes length is greater than 8, only the last 8 bytes are used.
func (bs Bytes) IntBE() Int { return intFromBytes(bs, binary.BigEndian) }

// IntLE interprets the Bytes as a signed 64-bit integer in LittleEndian order.
// If the Bytes length is less than 8, it is padded with trailing zeros.
// If the Bytes length is greater than 8, only the first 8 bytes are used.
func (bs Bytes) IntLE() Int { return intFromBytes(bs, binary.LittleEndian) }

// FloatBE interprets the Bytes as an IEEE-754 64-bit float in BigEndian order.
// If the Bytes length is not exactly 8, returns 0.
func (bs Bytes) FloatBE() Float {
	if len(bs) != 8 {
		return 0
	}

	bits := binary.BigEndian.Uint64(bs)

	return Float(math.Float64frombits(bits))
}

// FloatLE interprets the Bytes as an IEEE-754 64-bit float in LittleEndian order.
// If the Bytes length is not exactly 8, returns 0.
func (bs Bytes) FloatLE() Float {
	if len(bs) != 8 {
		return 0
	}

	bits := binary.LittleEndian.Uint64(bs)

	return Float(math.Float64frombits(bits))
}

// StripPrefix trims the specified Bytes prefix from the Bytes.
func (bs Bytes) StripPrefix(cutset Bytes) Bytes { return bytes.TrimPrefix(bs, cutset) }

// StripSuffix trims the specified Bytes suffix from the Bytes.
func (bs Bytes) StripSuffix(cutset Bytes) Bytes { return bytes.TrimSuffix(bs, cutset) }

// Split splits the Bytes by the specified separator and returns the iterator.
func (bs Bytes) Split(sep ...Bytes) SeqSlice[Bytes] {
	return transformSeq(
		bytes.SplitSeq(bs, Slice[Bytes](sep).Get(0).UnwrapOrDefault()),
		func(b []byte) Bytes { return Bytes(b) },
	)
}

// SplitAfter splits the Bytes after each instance of the specified separator and returns the iterator.
func (bs Bytes) SplitAfter(sep Bytes) SeqSlice[Bytes] {
	return transformSeq(bytes.SplitAfterSeq(bs, sep), func(b []byte) Bytes { return Bytes(b) })
}

// Fields splits the Bytes into a slice of substrings, removing any whitespace, and returns the iterator.
func (bs Bytes) Fields() SeqSlice[Bytes] {
	return transformSeq(bytes.FieldsSeq(bs), func(b []byte) Bytes { return Bytes(b) })
}

// FieldsBy splits the Bytes into a slice of substrings using a custom function to determine the field boundaries,
// and returns the iterator.
func (bs Bytes) FieldsBy(fn func(r rune) bool) SeqSlice[Bytes] {
	return transformSeq(bytes.FieldsFuncSeq(bs, fn), func(b []byte) Bytes { return Bytes(b) })
}

// Append appends the given Bytes to the current Bytes.
func (bs Bytes) Append(obs Bytes) Bytes { return append(bs, obs...) }

// Prepend prepends the given Bytes to the current Bytes.
func (bs Bytes) Prepend(obs Bytes) Bytes {
	if len(obs) == 0 {
		return bs
	}

	if len(bs) == 0 {
		return obs
	}

	out := make(Bytes, len(obs)+len(bs))
	copy(out, obs)
	copy(out[len(obs):], bs)

	return out
}

// Std returns the Bytes as a byte slice.
func (bs Bytes) Std() []byte { return bs }

// Clone creates a new Bytes instance with the same content as the current Bytes.
func (bs Bytes) Clone() Bytes { return bytes.Clone(bs) }

// Cmp compares the Bytes with another Bytes and returns an cmp.Ordering.
func (bs Bytes) Cmp(obs Bytes) cmp.Ordering { return cmp.Ordering(bytes.Compare(bs, obs)) }

// Contains checks if the Bytes contains the specified Bytes.
func (bs Bytes) Contains(obs Bytes) bool { return bytes.Contains(bs, obs) }

// ContainsAny checks if the Bytes contains any of the specified Bytes.
func (bs Bytes) ContainsAny(obss ...Bytes) bool {
	for _, obs := range obss {
		if bytes.Contains(bs, obs) {
			return true
		}
	}

	return false
}

// ContainsAll checks if the Bytes contains all of the specified Bytes.
func (bs Bytes) ContainsAll(obss ...Bytes) bool {
	for _, obs := range obss {
		if !bytes.Contains(bs, obs) {
			return false
		}
	}

	return true
}

// ContainsAnyChars checks if the given Bytes contains any characters from the input String.
func (bs Bytes) ContainsAnyChars(chars String) bool { return bytes.ContainsAny(bs, chars.Std()) }

// ContainsRune checks if the Bytes contains the specified rune.
func (bs Bytes) ContainsRune(r rune) bool { return bytes.ContainsRune(bs, r) }

// Count counts the number of occurrences of the specified Bytes in the Bytes.
func (bs Bytes) Count(obs Bytes) Int { return Int(bytes.Count(bs, obs)) }

// Empty checks if the Bytes is empty.
func (bs Bytes) Empty() bool { return len(bs) == 0 }

// Eq checks if the Bytes is equal to another Bytes.
func (bs Bytes) Eq(obs Bytes) bool { return bs.Cmp(obs).IsEq() }

// EqFold compares two Bytes slices case-insensitively.
func (bs Bytes) EqFold(obs Bytes) bool { return bytes.EqualFold(bs, obs) }

// Gt checks if the Bytes is greater than another Bytes.
func (bs Bytes) Gt(obs Bytes) bool { return bs.Cmp(obs).IsGt() }

// String returns the Bytes as an String.
func (bs Bytes) String() String { return String(bs) }

// StringUnsafe converts the Bytes into a String without copying memory.
// Warning: the resulting String shares the same underlying memory as the original Bytes.
// If the Bytes is modified later, the String will reflect those changes and may cause undefined behavior.
func (bs Bytes) StringUnsafe() String { return String(*(*string)(unsafe.Pointer(&bs))) }

// Index returns the index of the first instance of obs in bs, or -1 if bs is not present in obs.
func (bs Bytes) Index(obs Bytes) Int { return Int(bytes.Index(bs, obs)) }

// LastIndex returns the index of the last instance of obs in bs, or -1 if obs is not present in bs.
func (bs Bytes) LastIndex(obs Bytes) Int { return Int(bytes.LastIndex(bs, obs)) }

// IndexByte returns the index of the first instance of the byte b in bs, or -1 if b is not
// present in bs.
func (bs Bytes) IndexByte(b byte) Int { return Int(bytes.IndexByte(bs, b)) }

// LastIndexByte returns the index of the last instance of the byte b in bs, or -1 if b is not
// present in bs.
func (bs Bytes) LastIndexByte(b byte) Int { return Int(bytes.LastIndexByte(bs, b)) }

// IndexRune returns the index of the first instance of the rune r in bs, or -1 if r is not
// present in bs.
func (bs Bytes) IndexRune(r rune) Int { return Int(bytes.IndexRune(bs, r)) }

// Len returns the length of the Bytes.
func (bs Bytes) Len() Int { return Int(len(bs)) }

// LenRunes returns the number of runes in the Bytes.
func (bs Bytes) LenRunes() Int { return Int(utf8.RuneCount(bs)) }

// Lt checks if the Bytes is less than another Bytes.
func (bs Bytes) Lt(obs Bytes) bool { return bs.Cmp(obs).IsLt() }

// Map applies a function to each rune in the Bytes and returns the modified Bytes.
func (bs Bytes) Map(fn func(rune) rune) Bytes { return bytes.Map(fn, bs) }

// NormalizeNFC returns a new Bytes with its Unicode characters normalized using the NFC form.
func (bs Bytes) NormalizeNFC() Bytes { return norm.NFC.Bytes(bs) }

// Ne checks if the Bytes is not equal to another Bytes.
func (bs Bytes) Ne(obs Bytes) bool { return !bs.Eq(obs) }

// NotEmpty checks if the Bytes is not empty.
func (bs Bytes) NotEmpty() bool { return !bs.Empty() }

// Reader returns a *bytes.Reader initialized with the content of Bytes.
func (bs Bytes) Reader() *bytes.Reader { return bytes.NewReader(bs) }

// Repeat returns a new Bytes consisting of the current Bytes repeated 'count' times.
func (bs Bytes) Repeat(count Int) Bytes { return bytes.Repeat(bs, count.Std()) }

// Reset resets the length of the Bytes slice to zero, preserving its capacity.
func (bs *Bytes) Reset() { *bs = (*bs)[:0] }

// Runes returns the Bytes as a slice of runes.
func (bs Bytes) Runes() []rune { return bytes.Runes(bs) }

// Title converts the Bytes to title case.
func (bs Bytes) Title() Bytes { return title.Bytes(bs) }

// Lower converts the Bytes to lowercase.
func (bs Bytes) Lower() Bytes {
	for _, b := range bs {
		if b >= utf8.RuneSelf {
			return lower.Bytes(bs)
		}
	}

	needs := false

	for _, b := range bs {
		if 'A' <= b && b <= 'Z' {
			needs = true
			break
		}
	}

	if !needs {
		return bs
	}

	out := make(Bytes, len(bs))

	for i, b := range bs {
		if 'A' <= b && b <= 'Z' {
			out[i] = b + ('a' - 'A')
		} else {
			out[i] = b
		}
	}

	return out
}

// Upper converts the Bytes to uppercase.
func (bs Bytes) Upper() Bytes {
	for _, b := range bs {
		if b >= utf8.RuneSelf {
			return upper.Bytes(bs)
		}
	}

	needs := false

	for _, b := range bs {
		if 'a' <= b && b <= 'z' {
			needs = true
			break
		}
	}

	if !needs {
		return bs
	}

	out := make(Bytes, len(bs))

	for i, b := range bs {
		if 'a' <= b && b <= 'z' {
			out[i] = b - ('a' - 'A')
		} else {
			out[i] = b
		}
	}

	return out
}

// Print writes the content of the Bytes to the standard output (console)
// and returns the Bytes unchanged.
func (bs Bytes) Print() Bytes { fmt.Print(bs); return bs }

// Println writes the content of the Bytes to the standard output (console) with a newline
// and returns the Bytes unchanged.
func (bs Bytes) Println() Bytes { fmt.Println(bs); return bs }
