package model

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// NormalizeLanguageKey returns the key a recipient field value is matched on
// when it picks a language: trimmed, Unicode NFC and lower case. The frontend
// uses the same rule in normalizeLanguageKey (src/lib/utils/languages.js), so
// the trimmed characters are the ones String.prototype.trim removes.
func NormalizeLanguageKey(v string) string {
	v = strings.TrimFunc(v, isJSTrimSpace)
	v = norm.NFC.String(v)
	// a Caser keeps state and is not safe to share between goroutines
	return cases.Lower(language.Und).String(v)
}

// isJSTrimSpace reports whether r is white space or a line end as JavaScript
// trim sees it
func isJSTrimSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}
