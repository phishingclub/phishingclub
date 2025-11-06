package iter

import "iter"

// Seq represents a single-value iterator sequence.
// It calls the yield function for each element in the sequence.
// If yield returns false, the iteration should stop.
type Seq[T any] iter.Seq[T]

// Seq2 represents a two-value iterator sequence (typically key-value pairs).
// It calls the yield function for each pair in the sequence.
// If yield returns false, the iteration should stop.
type Seq2[K, V any] iter.Seq2[K, V]

// Pair represents a key-value pair.
// Used for converting between Seq2 and slice representations.
type Pair[K, V any] struct {
	Key   K
	Value V
}

// Integer defines numeric types that can be used with Iota generators.
// Supports all signed and unsigned integer types.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}
