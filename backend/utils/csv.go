package utils

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CSVRemoveFormulaStart neutralises a cell a spreadsheet would run as a
// formula, by putting a quote in front of a value that starts with = + - @ tab
// carriage return or new line, or that has a formula start as its first
// character after white space. White space is any Unicode space, such as a
// no break space, and the zero width space and byte order mark. A formula
// start is one of = + - @ or its fullwidth form, which some spreadsheets read
// the same way.
func CSVRemoveFormulaStart(input string) string {
	if input == "" {
		return input
	}
	switch input[0] {
	case '\t', '\r', '\n':
		return "'" + input
	}
	trimmed := strings.TrimLeftFunc(input, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\u200b' || r == '\ufeff'
	})
	if trimmed == "" {
		return input
	}
	first, _ := utf8.DecodeRuneInString(trimmed)
	switch first {
	case '=', '+', '-', '@', '\uff1d', '\uff0b', '\uff0d', '\uff20':
		return "'" + input
	}
	return input
}

func CSVFromDate(d *time.Time) string {
	if d == nil {
		return ""
	}
	return CSVRemoveFormulaStart(d.Format(time.RFC3339))
}
