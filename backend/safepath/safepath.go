// Package safepath checks relative file paths that come from packages and
// imported archives before they are used for lookups, database rows or disk
// access. A path is rejected rather than cleaned, so the value that was checked
// is the exact value used everywhere after.
package safepath

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Check returns an error unless p is a strict relative path: valid UTF-8, not
// absolute, no backslash or NUL, no control or format characters, equal to its
// cleaned form, and every segment non empty, not "." or "..", and without
// leading or trailing white space.
func Check(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("path is not valid UTF-8")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("absolute path %q", p)
	}
	for _, r := range p {
		switch {
		case r == '\\':
			return fmt.Errorf("path %q has a backslash", p)
		case r == 0, unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			return fmt.Errorf("path %q has a control or format character %U", p, r)
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("path %q has an empty, \".\" or \"..\" segment", p)
		}
		first, _ := utf8.DecodeRuneInString(seg)
		last, _ := utf8.DecodeLastRuneInString(seg)
		if unicode.IsSpace(first) || unicode.IsSpace(last) {
			return fmt.Errorf("path %q has a segment that starts or ends with white space", p)
		}
	}
	if path.Clean(p) != p {
		return fmt.Errorf("path %q is not in clean form", p)
	}
	return nil
}

// FoldKey returns the key two paths share when they differ only by letter
// case, so callers can refuse such pairs on case insensitive file systems.
func FoldKey(p string) string {
	return strings.ToLower(p)
}
