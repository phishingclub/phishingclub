package g

import (
	"os"
	"sync"

	"github.com/enetx/g/cmp"
	"github.com/enetx/iter"
)

type (
	// Result is a generic struct for representing a result value along with an error.
	Result[T any] struct {
		v   T     // Value.
		err error // Associated error.
	}

	// Option is a generic struct for representing an optional value.
	Option[T any] struct {
		v      T    // Value.
		isSome bool // Indicator of value presence.
	}

	// Unit represents an empty value.
	// Used in contexts where a function needs to return "something" but
	// the actual value doesn't matter, only success/failure status.
	Unit struct{}

	// File is a struct that represents a file along with an iterator for reading lines.
	File struct {
		file  *os.File // Underlying os.File.
		name  String   // File name.
		guard bool     // Guard indicates whether the file is protected against concurrent access.
	}

	// Dir is a struct representing a directory path.
	Dir struct {
		path String // Directory path.
	}

	// String is an alias for the string type.
	String string

	// Int is an alias for the int type.
	Int int

	// Float is an alias for the float64 type.
	Float float64

	// Bytes is an alias for the []byte type.
	Bytes []byte

	// Slice is a generic alias for a slice.
	Slice[T any] []T

	// Map is a generic alias for a map.
	Map[K comparable, V any] map[K]V

	// MapEntry provides a view into a single key of a Map.
	// It exposes a fluent, chain-friendly interface for inspecting, inserting,
	// mutating, or deleting a value with a single key lookup.
	MapEntry[K comparable, V any] struct {
		m   Map[K, V]
		key K
	}

	// MapSafeEntry provides a view into a single key of a concurrent-safe Map.
	// It exposes a fluent, chain-friendly interface for inspecting, inserting,
	// mutating, or deleting a value with a single key lookup.
	MapSafeEntry[K comparable, V any] struct {
		m   *MapSafe[K, V]
		key K
	}

	// Set is a generic alias for a set implemented using a map.
	Set[T comparable] map[T]struct{}

	// Pair is a struct representing a key-value Pair for MapOrd.
	Pair[K, V any] = iter.Pair[K, V]

	// MapOrd is an ordered map that maintains insertion order using a slice for pairs
	// and a map for fast index lookups.
	MapOrd[K comparable, V any] []Pair[K, V] // ordered key-value pairs

	// MapOrdEntry provides a view into a single key of an ordered Map (MapOrd),
	// enabling fluent insertion, mutation, and deletion while preserving entry order.
	MapOrdEntry[K comparable, V any] struct {
		mo  *MapOrd[K, V]
		key K
	}

	// MapSafe is a concurrent-safe generic map built on sync.Map.
	MapSafe[K comparable, V any] struct {
		data sync.Map
	}

	// Heap is a generic binary heap data structure that maintains elements in heap order.
	// It can be configured as either a min-heap or max-heap based on the comparison function.
	Heap[T any] struct {
		data Slice[T]
		cmp  func(T, T) cmp.Ordering
	}

	// Deque is a double-ended queue implemented with a growable ring buffer.
	// It provides efficient insertion and removal of elements at both ends.
	Deque[T any] struct {
		data  Slice[T]
		front Int
		len   Int
	}

	// Named is a map-like type that stores key-value pairs for resolving named
	// placeholders in Sprintf.
	Named Map[String, any]

	// SeqSet is an iterator over sequences of unique values.
	SeqSet[V comparable] iter.Seq[V]

	// SeqSlice is an iterator over sequences of individual values.
	SeqSlice[V any] iter.Seq[V]

	// SeqHeap is an iterator over sequences of Heap values.
	SeqHeap[V any] iter.Seq[V]

	// SeqDeque is an iterator over sequences of Deque values.
	SeqDeque[V any] iter.Seq[V]

	// SeqResult is an iterator over sequences of Result[V] values.
	SeqResult[V any] iter.Seq[Result[V]]

	// SeqSlices is an iterator over slices of sequences of individual values.
	SeqSlices[V any] iter.Seq[[]V]

	// SeqMapOrd is an iterator over sequences of ordered pairs of values, most commonly ordered key-value pairs.
	SeqMapOrd[K comparable, V any] iter.Seq2[K, V]

	// SeqMap is an iterator over sequences of pairs of values, most commonly key-value pairs.
	SeqMap[K comparable, V any] iter.Seq2[K, V]

	// SeqSlicePar is a parallel iterator over a slice of elements of type T.
	// It uses a fixed-size pool of worker goroutines to process elements concurrently.
	SeqSlicePar[V any] struct {
		seq     SeqSlice[V]
		workers Int
		process func(V) (V, bool)
	}

	// SeqMapPar is the parallel version of SeqMap[K,V].
	SeqMapPar[K comparable, V any] struct {
		seq     SeqMap[K, V]
		workers Int
		process func(Pair[K, V]) (Pair[K, V], bool)
	}

	// SeqDequePar is a parallel iterator over a deque of elements of type T.
	// It uses a fixed-size pool of worker goroutines to process elements concurrently.
	SeqDequePar[V any] struct {
		seq     SeqDeque[V]
		workers Int
		process func(V) (V, bool)
	}

	// SeqHeapPar is a parallel iterator over a heap of elements of type T.
	// It uses a fixed-size pool of worker goroutines to process elements concurrently.
	SeqHeapPar[V any] struct {
		seq     SeqHeap[V]
		workers Int
		process func(V) (V, bool)
	}
)
