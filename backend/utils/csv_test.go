package utils

import "testing"

func TestCSVRemoveFormulaStart(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"plain":      "plain",
		"=1+1":       "'=1+1",
		"+1":         "'+1",
		"-1":         "'-1",
		"@SUM(A1)":   "'@SUM(A1)",
		"\t=1":       "'\t=1",
		"\r=1":       "'\r=1",
		"\n=1":       "'\n=1",
		"  =1":       "'  =1",
		"a=b":        "a=b",
		"2026-11-13": "2026-11-13",
		" =cmd":      "' =cmd",
		" \t@x":      "' \t@x",
		" alice":     " alice",
		"   ":        "   ",
		// unicode spaces in front
		"\u00a0=1":     "'\u00a0=1",
		"\u2003+1":     "'\u2003+1",
		"\u3000@x":     "'\u3000@x",
		"\u200b=1":     "'\u200b=1",
		"\ufeff-1":     "'\ufeff-1",
		"\u00a0alice":  "\u00a0alice",
		"\u00a0\u00a0": "\u00a0\u00a0",
		// fullwidth formula starts
		"\uff1dSUM(A1)":  "'\uff1dSUM(A1)",
		"\uff0b1":        "'\uff0b1",
		"\uff0d1":        "'\uff0d1",
		"\uff20x":        "'\uff20x",
		" \u00a0\uff1d1": "' \u00a0\uff1d1",
		"\u00e9=1":       "\u00e9=1",
	}
	for in, want := range cases {
		if got := CSVRemoveFormulaStart(in); got != want {
			t.Errorf("CSVRemoveFormulaStart(%q) = %q, want %q", in, got, want)
		}
	}
}
