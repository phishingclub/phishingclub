package iter

// FromSlice creates a sequence that iterates over the given slice in forward order.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3})
//	iter.ForEach(s, func(x int) { fmt.Println(x) })
//	// Output:
//	// 1
//	// 2
//	// 3
func FromSlice[T any](sl []T) Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range sl {
			if !yield(v) {
				return
			}
		}
	}
}

// FromSliceReverse creates a sequence that iterates over the given slice in reverse order.
// Note: This does NOT allocate or collect; it walks the provided slice backwards.
//
// Example:
//
//	s := iter.FromSliceReverse([]int{1, 2, 3})
//	iter.ForEach(s, func(x int) { fmt.Println(x) })
//	// Output:
//	// 3
//	// 2
//	// 1
func FromSliceReverse[T any](sl []T) Seq[T] {
	return func(yield func(T) bool) {
		for i := len(sl) - 1; i >= 0; i-- {
			if !yield(sl[i]) {
				return
			}
		}
	}
}

// FromChan creates a sequence from a channel.
// The sequence will stop when the channel is closed.
//
// Example:
//
//	ch := make(chan int)
//	go func() {
//	  defer close(ch)
//	  for i := 0; i < 3; i++ { ch <- i }
//	}()
//	s := iter.FromChan(ch)
func FromChan[T any](ch <-chan T) Seq[T] {
	return func(yield func(T) bool) {
		for v := range ch {
			if !yield(v) {
				return
			}
		}
	}
}

// FromMap creates a sequence from a map.
// The order of key-value pairs is not guaranteed.
//
// Example:
//
//	m := map[int]string{1: "a", 2: "b", 3: "c"}
//	s := iter.FromMap(m)
func FromMap[K comparable, V any](m map[K]V) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range m {
			if !yield(k, v) {
				return
			}
		}
	}
}

// FromPairs creates a Seq2 from a slice of key-value pairs.
//
// Example:
//
//	pairs := []Pair[int, string]{{1, "a"}, {2, "b"}}
//	s := iter.FromPairs(pairs)
func FromPairs[K, V any](pairs []Pair[K, V]) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, p := range pairs {
			if !yield(p.Key, p.Value) {
				return
			}
		}
	}
}
