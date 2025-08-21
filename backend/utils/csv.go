package utils

import (
	"strings"
	"time"
)

func CSVRemoveFormulaStart(input string) string {
	if input == "" {
		return input
	}
	if len(input) > 0 && strings.ContainsAny(input[0:1], "=@+-") {
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
