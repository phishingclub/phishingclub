package utils

import (
	"fmt"
	"strings"
)

// Uint8Arr redefines how []uint8 is marshalled to JSON
// in order to display it as a list of numbers instead of a string
type Uint8Arr []uint8

func (u Uint8Arr) MarshalJSON() ([]byte, error) {
	var result string
	if u == nil {
		result = "[]"
	} else {
		result = strings.Join(strings.Fields(fmt.Sprintf("%d", u)), ",")
	}
	return []byte(result), nil
}
