package utils

import (
	"path"
	"strconv"

	"github.com/phishingclub/phishingclub/errs"
)

// Substring returns a substring of the input text from the start index to the end index.
// If the start index is less than 0, it is set to 0.
// If the end index is greater than the length of the text, it is set to the length of the text.
func Substring(text string, start int, end int) string {
	// Validate start and end indexes (within 0 to string length)
	if start < 0 {
		start = 0
	} else if start > len(text) {
		start = len(text)
	}
	if end < 0 {
		end = 0
	} else if end > len(text) {
		end = len(text)
	}
	if start > end {
		return ""
	}
	return text[start:end]
}

// MergeStringMaps merges multiple string maps into a single map by copying all key-value pairs.
// If the same key exists in multiple maps, the last map's value will overwrite previous values.
func MergeStringMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// MergeStringSlices merges multiple string slices into a single slice by copying all elements.
func MergeStringSlices(slices ...[]string) []string {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	result := make([]string, 0, total)
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}

// TODO maybe move this to a file utils file
func CompareFileSizeFromString(fileSize int64, maxSizeInMB string) (bool, error) {
	maxFileSizeMB, err := strconv.Atoi(maxSizeInMB)
	if err != nil {
		return false, errs.Wrap(err)
	}
	maxSizeBytes := maxFileSizeMB * 1024 * 1024
	if fileSize > int64(maxSizeBytes) {
		return false, nil
	}
	return true, nil
}

// TODO maybe move this to a file utils file
func ReadableFileName(filename string) string {
	maxLength := 24
	name := path.Base(filename)
	if len(name) <= maxLength {
		return name
	}

	// Keep equal parts from start and end
	// Example: "very-long-filename-123.pdf" -> "very-l...123.pdf"
	ext := path.Ext(name)                 // gets ".pdf"
	basename := name[:len(name)-len(ext)] // removes extension

	// Calculate how many chars to keep on each end
	// -3 for "..." and divide remaining space by 2
	keepLength := (maxLength - 3 - len(ext)) / 2

	return basename[:keepLength] + "..." + basename[len(basename)-keepLength:] + ext
}
