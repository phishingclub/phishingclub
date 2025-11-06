package utils

import "sort"

type SliceType interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

func DedupIntArr[T SliceType](arr []T) []T {
	// Sort the array
	sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })

	// Dedup
	j := 0
	for i := 1; i < len(arr); i++ {
		if arr[j] != arr[i] {
			j++
			arr[j] = arr[i]
		}
	}
	return arr[:j+1]
}
