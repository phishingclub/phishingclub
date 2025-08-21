package utils

import (
	"fmt"

	"github.com/oapi-codegen/nullable"
)

// NullableToString converts a nullable stringer to a string
func NullableToString[T fmt.Stringer](x nullable.Nullable[T]) string {
	if !x.IsSpecified() || x.IsNull() {
		return ""
	}
	return x.MustGet().String()
}
