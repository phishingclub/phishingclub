// Package lure builds and resolves the short recipient identifiers that appear
// in a lure URL, for example https://example.com/account/4H7K9QM2XR3T.
//
// A code is stored and matched byte for byte. Base58 treats the two cases as
// different symbols, so folding would collapse distinct codes, and applying the
// same rule to every algorithm keeps one matching behaviour rather than one per
// alphabet.
package lure

import "strings"

// Alphabet is the Crockford base32 alphabet. It omits I, L, O and U so a code
// read aloud cannot be confused with a similar glyph or spell a word.
const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Base58Alphabet is the Bitcoin base58 alphabet. Keeping both cases needs fewer
// characters for the same number of combinations, and 0, O, I and l are dropped
// as hard to tell apart in print.
const Base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// Bounds on a generated code. The lower bound is low enough to be guessable,
// which is an operator choice for short lived campaigns.
const (
	MinLength     = 6
	MaxLength     = 16
	DefaultLength = 12
)

// MaxCustomLength is the only limit on an operator written code, and it exists
// because the column is varchar(64).
const MaxCustomLength = 64

// DisallowedCustomCharacters lists what IsCandidate rejects, as the symbols
// themselves, so an error message shows what to look for.
const DisallowedCustomCharacters = `/ \ % ? # " ' < >`

// IsCandidate reports whether a path segment or query value could be a lure code
// and is therefore worth a database probe.
//
// An operator written code may be any text, so no alphabet applies. This rules
// out only what could never be a code: too long for the column, or needing an
// escape to sit in a single path segment. A single dot is allowed so a lure can
// end in invoice.pdf, at the cost of one indexed probe per static asset request.
func IsCandidate(s string) bool {
	if s == "" || len(s) > MaxCustomLength {
		return false
	}
	// a browser resolves a lone dot away before sending, and LastPathSegment
	// refuses a whole path carrying a doubled dot rather than cleaning it, so
	// neither shape could arrive back intact
	if s == "." || strings.Contains(s, "..") {
		return false
	}
	// would split the segment or need escaping
	if strings.ContainsAny(s, "/%?#\\") {
		return false
	}
	// the finished URL is written into a mail body rendered with text/template,
	// which does not escape it
	if strings.ContainsAny(s, "\"'<>") {
		return false
	}
	// a code carrying one could never be matched back from a request path
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// IsValidCustom reports whether an operator supplied code can be stored. The
// rules are the same ones that keep a code reachable, so a stored code always
// resolves. A path with more than one segment comes from the template URL path,
// not from the code.
func IsValidCustom(s string) bool {
	return IsCandidate(s)
}
