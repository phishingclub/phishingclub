package utils

func SliceRemoveInt(intSlice []int, target int) []int {
	result := []int{}
	for _, num := range intSlice {
		if num != target {
			result = append(result, num)
		}
	}
	return result
}
