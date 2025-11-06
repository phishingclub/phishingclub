package g

import (
	"regexp"

	"github.com/enetx/g/f"
)

// regexps struct wraps a String and provides regex-related methods.
type regexps struct{ str String }

// Regexp wraps a String into an re struct to provide regex-related methods.
func (s String) Regexp() regexps { return regexps{s} }

// Find searches the String for the first occurrence of the regulare xpression pattern
// and returns an Option[String] containing the matched substring.
// If no match is found, it returns None.
func (r regexps) Find(pattern *regexp.Regexp) Option[String] {
	result := String(pattern.FindString(r.str.Std()))
	if result.Empty() {
		return None[String]()
	}

	return Some(result)
}

// Replace replaces all occurrences of the regular expression matches in the String
// with the provided newS (as a String) and returns the resulting String after the replacement.
func (r regexps) Replace(pattern *regexp.Regexp, newS String) String {
	return String(pattern.ReplaceAllString(r.str.Std(), newS.Std()))
}

// ReplaceBy replaces all occurrences of the regular expression matches in the String
// by applying a custom transformation function to each match.
// The function `fn` takes a String representing a match and returns a String that will replace it.
func (r regexps) ReplaceBy(pattern *regexp.Regexp, fn func(match String) String) String {
	return String(pattern.ReplaceAllStringFunc(r.str.Std(), func(s string) string { return fn(String(s)).Std() }))
}

// Match checks if the String contains a match for the specified regular expression pattern.
func (r regexps) Match(pattern *regexp.Regexp) bool { return f.Match[String](pattern)(r.str) }

// MatchAny checks if the String contains a match for any of the specified regular
// expression patterns.
func (r regexps) MatchAny(patterns ...*regexp.Regexp) bool {
	return Slice[*regexp.Regexp](patterns).
		Iter().
		Any(func(pattern *regexp.Regexp) bool { return r.Match(pattern) })
}

// MatchAll checks if the String contains a match for all of the specified regular expression patterns.
func (r regexps) MatchAll(patterns ...*regexp.Regexp) bool {
	return Slice[*regexp.Regexp](patterns).
		Iter().
		All(func(pattern *regexp.Regexp) bool { return r.Match(pattern) })
}

// Split splits the String into substrings using the provided regular expression pattern and returns an Slice[String] of the results.
// The regular expression pattern is provided as a regexp.Regexp parameter.
func (r regexps) Split(pattern *regexp.Regexp) Slice[String] {
	return TransformSlice(pattern.Split(r.str.Std(), -1), NewString)
}

// SplitN splits the String into substrings using the provided regular expression pattern and returns an Slice[String] of the results.
// The regular expression pattern is provided as a regexp.Regexp parameter.
// The n parameter controls the number of substrings to return:
// - If n is negative, there is no limit on the number of substrings returned.
// - If n is zero, an empty Slice[String] is returned.
// - If n is positive, at most n substrings are returned.
func (r regexps) SplitN(pattern *regexp.Regexp, n Int) Option[Slice[String]] {
	result := TransformSlice(pattern.Split(r.str.Std(), n.Std()), NewString)
	if result.Empty() {
		return None[Slice[String]]()
	}

	return Some(result)
}

// RxIndex searches for the first occurrence of the regular expression pattern in the String.
// If a match is found, it returns an Option containing an Slice with the start and end indices of the match.
// If no match is found, it returns None.
func (r regexps) Index(pattern *regexp.Regexp) Option[Slice[Int]] {
	result := TransformSlice(pattern.FindStringIndex(r.str.Std()), NewInt)
	if result.Empty() {
		return None[Slice[Int]]()
	}

	return Some(result)
}

// FindAll searches the String for all occurrences of the regular expression pattern
// and returns an Option[Slice[String]] containing a slice of matched substrings.
// If no matches are found, the Option[Slice[String]] will be None.
func (r regexps) FindAll(pattern *regexp.Regexp) Option[Slice[String]] {
	return r.FindAllN(pattern, -1)
}

// FindAllN searches the String for up to n occurrences of the regular expression pattern
// and returns an Option[Slice[String]] containing a slice of matched substrings.
// If no matches are found, the Option[Slice[String]] will be None.
// If n is negative, all occurrences will be returned.
func (r regexps) FindAllN(pattern *regexp.Regexp, n Int) Option[Slice[String]] {
	result := TransformSlice(pattern.FindAllString(r.str.Std(), n.Std()), NewString)
	if result.Empty() {
		return None[Slice[String]]()
	}

	return Some(result)
}

// FindSubmatch searches the String for the first occurrence of the regular expression pattern
// and returns an Option[Slice[String]] containing the matched substrings and submatches.
// The Option will contain an Slice[String] with the full match at index 0, followed by any captured submatches.
// If no match is found, it returns None.
func (r regexps) FindSubmatch(pattern *regexp.Regexp) Option[Slice[String]] {
	result := TransformSlice(pattern.FindStringSubmatch(r.str.Std()), NewString)
	if result.Empty() {
		return None[Slice[String]]()
	}

	return Some(result)
}

// FindAllSubmatch searches the String for all occurrences of the regular expression pattern
// and returns an Option[Slice[Slice[String]]] containing the matched substrings and submatches.
// The Option[Slice[Slice[String]]] will contain an Slice[String] for each match,
// where each Slice[String] will contain the full match at index 0, followed by any captured submatches.
// If no match is found, the Option[Slice[Slice[String]]] will be None.
// This method is equivalent to calling SubmatchAllRegexpN with n = -1, which means it finds all occurrences.
func (r regexps) FindAllSubmatch(pattern *regexp.Regexp) Option[Slice[Slice[String]]] {
	return r.FindAllSubmatchN(pattern, -1)
}

// FindAllSubmatchN searches the String for occurrences of the regular expression pattern
// and returns an Option[Slice[Slice[String]]] containing the matched substrings and submatches.
// The Option[Slice[Slice[String]]] will contain an Slice[String] for each match,
// where each Slice[String] will contain the full match at index 0, followed by any captured submatches.
// If no match is found, the Option[Slice[Slice[String]]] will be None.
// The 'n' parameter specifies the maximum number of matches to find. If n is negative, it finds all occurrences.
func (r regexps) FindAllSubmatchN(pattern *regexp.Regexp, n Int) Option[Slice[Slice[String]]] {
	var result Slice[Slice[String]]

	for _, v := range pattern.FindAllStringSubmatch(r.str.Std(), n.Std()) {
		result = append(result, TransformSlice(v, NewString))
	}

	if result.Empty() {
		return None[Slice[Slice[String]]]()
	}

	return Some(result)
}

// Compile compiles the String into a regular expression (regexp.Regexp).
//
// This method attempts to compile the String receiver into a regular expression using the
// regexp.Compile function from the standard library. If the compilation is successful,
// the function returns a Result containing the compiled *regexp.Regexp. If the compilation
// fails due to an invalid regular expression pattern, the Result will contain the error.
//
// Returns:
// - Result[*regexp.Regexp]: A Result containing the compiled *regexp.Regexp if successful, or an error otherwise.
//
// Example usage:
//
//	s := g.String(`^\d+$`)
//	compiledRegex := s.Regexp().Compile()
//	if compiledRegex.IsOk() {
//	    fmt.Println("Regex compiled successfully")
//	} else {
//	    fmt.Println("Failed to compile regex:", compiledRegex.Err())
//	}
func (r regexps) Compile() Result[*regexp.Regexp] { return ResultOf(regexp.Compile(r.str.Std())) }
