package g

import (
	"regexp"

	"github.com/enetx/g/f"
)

// regexps struct wraps a Bytes and provides regex-related methods.
type regexpb struct{ bytes Bytes }

// Regexp wraps a Bytes into an re struct to provide regex-related methods.
func (bs Bytes) Regexp() regexpb { return regexpb{bs} }

// Find searches the Bytes for the first occurrence of the regular expression pattern
// and returns an Option[Bytes] containing the matched substring.
// If no match is found, the Option[Bytes] will be None.
func (r regexpb) Find(pattern *regexp.Regexp) Option[Bytes] {
	result := Bytes(pattern.Find(r.bytes))
	if result.Empty() {
		return None[Bytes]()
	}

	return Some(result)
}

// Match checks if the Bytes contains a match for the specified regular expression pattern.
func (r regexpb) Match(pattern *regexp.Regexp) bool { return f.Match[Bytes](pattern)(r.bytes) }

// MatchAny checks if the Bytes contains a match for any of the specified regular
// expression patterns.
func (r regexpb) MatchAny(patterns ...*regexp.Regexp) bool {
	return Slice[*regexp.Regexp](patterns).
		Iter().
		Any(func(pattern *regexp.Regexp) bool { return r.Match(pattern) })
}

// MatchAll checks if the Bytes contains a match for all of the specified regular expression patterns.
func (r regexpb) MatchAll(patterns ...*regexp.Regexp) bool {
	return Slice[*regexp.Regexp](patterns).
		Iter().
		All(func(pattern *regexp.Regexp) bool { return r.Match(pattern) })
}

// Index searches for the first occurrence of the regular expression pattern in the Bytes.
// If a match is found, it returns an Option containing an Slice with the start and end indices of the match.
// If no match is found, it returns None.
func (r regexpb) Index(pattern *regexp.Regexp) Option[Slice[Int]] {
	result := TransformSlice(pattern.FindIndex(r.bytes), NewInt)
	if result.Empty() {
		return None[Slice[Int]]()
	}

	return Some(result)
}

// FindAll searches the Bytes for all occurrences of the regular expression pattern
// and returns an Option[Slice[Bytes]] containing a slice of matched substrings.
// If no matches are found, the Option[Slice[Bytes]] will be None.
func (r regexpb) FindAll(pattern *regexp.Regexp) Option[Slice[Bytes]] {
	return r.FindAllN(pattern, -1)
}

// FindAllN searches the Bytes for up to n occurrences of the regular expression pattern
// and returns an Option[Slice[Bytes]] containing a slice of matched substrings.
// If no matches are found, the Option[Slice[Bytes]] will be None.
// If n is negative, all occurrences will be returned.
func (r regexpb) FindAllN(pattern *regexp.Regexp, n Int) Option[Slice[Bytes]] {
	result := TransformSlice(pattern.FindAll(r.bytes, n.Std()), func(bs []byte) Bytes { return Bytes(bs) })
	if result.Empty() {
		return None[Slice[Bytes]]()
	}

	return Some(result)
}

// FindSubmatch searches the Bytes for the first occurrence of the regular expression pattern
// and returns an Option[Slice[Bytes]] containing the matched substrings and submatches.
// The Option[Slice[Bytes]] will contain an Slice[Bytes] for each match,
// where each Slice[Bytes] will contain the full match at index 0, followed by any captured submatches.
// If no match is found, the Option[Slice[Bytes]] will be None.
func (r regexpb) FindSubmatch(pattern *regexp.Regexp) Option[Slice[Bytes]] {
	result := TransformSlice(pattern.FindSubmatch(r.bytes), func(bs []byte) Bytes { return Bytes(bs) })
	if result.Empty() {
		return None[Slice[Bytes]]()
	}

	return Some(result)
}

// FindAllSubmatch searches the Bytes for all occurrences of the regular expression pattern
// and returns an Option[Slice[Slice[Bytes]]] containing the matched substrings and submatches.
// The Option[Slice[Slice[Bytes]]] will contain an Slice[Bytes] for each match,
// where each Slice[Bytes] will contain the full match at index 0, followed by any captured submatches.
// If no match is found, the Option[Slice[Slice[Bytes]]] will be None.
// This method is equivalent to calling SubmatchAllRegexpN with n = -1, which means it finds all occurrences.
func (r regexpb) FindAllSubmatch(pattern *regexp.Regexp) Option[Slice[Slice[Bytes]]] {
	return r.FindAllSubmatchN(pattern, -1)
}

// FindAllSubmatchN searches the Bytes for occurrences of the regular expression pattern
// and returns an Option[Slice[Slice[Bytes]]] containing the matched substrings and submatches.
// The Option[Slice[Slice[Bytes]]] will contain an Slice[Bytes] for each match,
// where each Slice[Bytes] will contain the full match at index 0, followed by any captured submatches.
// If no match is found, the Option[Slice[Slice[Bytes]]] will be None.
// The 'n' parameter specifies the maximum number of matches to find. If n is negative, it finds all occurrences.
func (r regexpb) FindAllSubmatchN(pattern *regexp.Regexp, n Int) Option[Slice[Slice[Bytes]]] {
	var result Slice[Slice[Bytes]]

	for _, v := range pattern.FindAllSubmatch(r.bytes, n.Std()) {
		result = append(result, TransformSlice(v, func(bs []byte) Bytes { return Bytes(bs) }))
	}

	if result.Empty() {
		return None[Slice[Slice[Bytes]]]()
	}

	return Some(result)
}

// Replace replaces all occurrences of the regular expression matches in the Bytes
// with the provided newB and returns the resulting Bytes after the replacement.
func (r regexpb) Replace(pattern *regexp.Regexp, newB Bytes) Bytes {
	return pattern.ReplaceAll(r.bytes, newB)
}
