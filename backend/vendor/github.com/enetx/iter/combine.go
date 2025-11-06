package iter

// Chain concatenates multiple sequences into one.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2})
//	s2 := iter.FromSlice([]int{3, 4})
//	iter.Chain(s1, s2) // yields: 1, 2, 3, 4
func Chain[T any](head Seq[T], rest ...Seq[T]) Seq[T] {
	return func(yield func(T) bool) {
		// Process head sequence
		continueProcessing := true
		head(func(v T) bool {
			if !yield(v) {
				continueProcessing = false
				return false
			}
			return true
		})

		if !continueProcessing {
			return
		}

		// Process rest sequences
		for _, s := range rest {
			s(func(v T) bool {
				if !yield(v) {
					continueProcessing = false
					return false
				}
				return true
			})

			if !continueProcessing {
				return
			}
		}
	}
}

// Zip combines two sequences into pairs, stopping when either sequence ends.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]string{"a", "b"})
//	iter.Zip(s1, s2) // yields: (1, "a"), (2, "b")
func Zip[A, B any](a Seq[A], b Seq[B]) Seq2[A, B] {
	return func(y func(A, B) bool) {
		an, as := Pull(a)
		defer as()
		bn, bs := Pull(b)
		defer bs()
		for {
			av, ok := an()
			if !ok {
				return
			}
			bv, ok := bn()
			if !ok {
				return
			}
			if !y(av, bv) {
				return
			}
		}
	}
}

// ZipWith combines two sequences using a function, stopping when either sequence ends.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{10, 20, 30})
//	iter.ZipWith(s1, s2, func(a, b int) int { return a + b }) // yields: 11, 22, 33
func ZipWith[A, B, R any](a Seq[A], b Seq[B], f func(A, B) R) Seq[R] {
	return func(yield func(R) bool) {
		an, as := Pull(a)
		defer as()
		bn, bs := Pull(b)
		defer bs()
		for {
			av, ok := an()
			if !ok {
				return
			}
			bv, ok := bn()
			if !ok {
				return
			}
			if !yield(f(av, bv)) {
				return
			}
		}
	}
}

// Interleave alternates between elements from two sequences.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{10, 20, 30})
//	iter.Interleave(s1, s2) // yields: 1, 10, 2, 20, 3, 30
func Interleave[T any](a, b Seq[T]) Seq[T] {
	return func(yield func(T) bool) {
		an, as := Pull(a)
		defer as()
		bn, bs := Pull(b)
		defer bs()

		for {
			av, aok := an()
			if aok {
				if !yield(av) {
					return
				}
			}

			bv, bok := bn()
			if bok {
				if !yield(bv) {
					return
				}
			}

			if !aok && !bok {
				return
			}
		}
	}
}

// Windows returns a sequence of sliding windows of size n.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3, 4, 5})
//	iter.Windows(s, 3) // yields: [1,2,3], [2,3,4], [3,4,5]
func Windows[T any](s Seq[T], n int) Seq[[]T] {
	return func(yield func([]T) bool) {
		if n <= 0 {
			return
		}

		window := make([]T, 0, n)
		s(func(v T) bool {
			window = append(window, v)
			if len(window) == n {
				windowCopy := make([]T, n)
				copy(windowCopy, window)
				if !yield(windowCopy) {
					return false
				}
				window = window[1:]
			}
			return true
		})
	}
}

// Chunks returns a sequence of chunks of size n.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3, 4, 5})
//	iter.Chunks(s, 2) // yields: [1,2], [3,4], [5]
func Chunks[T any](s Seq[T], n int) Seq[[]T] {
	return func(yield func([]T) bool) {
		if n <= 0 {
			return
		}

		chunk := make([]T, 0, n)
		s(func(v T) bool {
			chunk = append(chunk, v)
			if len(chunk) == n {
				if !yield(chunk) {
					return false
				}
				chunk = make([]T, 0, n)
			}
			return true
		})

		if len(chunk) > 0 {
			yield(chunk)
		}
	}
}

// GroupByAdjacent groups consecutive elements that are considered the same by the comparison function.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 1, 2, 2, 2, 3})
//	iter.GroupByAdjacent(s, func(a, b int) bool { return a == b })
//	// yields: [1,1], [2,2,2], [3]
func GroupByAdjacent[T any](s Seq[T], same func(a, b T) bool) Seq[[]T] {
	return func(yield func([]T) bool) {
		var group []T
		var prev T
		first := true
		cont := true

		s(func(v T) bool {
			if first {
				group = []T{v}
				prev = v
				first = false
				return true
			}
			if same(prev, v) {
				group = append(group, v)
				prev = v
				return true
			}

			out := make([]T, len(group))
			copy(out, group)
			if !yield(out) {
				cont = false
				return false
			}
			group = []T{v}
			prev = v
			return true
		})

		if cont && len(group) > 0 {
			out := make([]T, len(group))
			copy(out, group)
			_ = yield(out)
		}
	}
}

// Chain2 concatenates multiple Seq2 sequences into one.
//
// Example:
//
//	s1 := iter.FromMap(map[int]string{1: "a"})
//	s2 := iter.FromMap(map[int]string{2: "b"})
//	iter.Chain2(s1, s2) // yields: (1, "a"), (2, "b")
func Chain2[K, V any](head Seq2[K, V], rest ...Seq2[K, V]) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		// Process head sequence
		continueProcessing := true
		head(func(k K, v V) bool {
			if !yield(k, v) {
				continueProcessing = false
				return false
			}
			return true
		})

		if !continueProcessing {
			return
		}

		for _, s := range rest {
			s(func(k K, v V) bool {
				if !yield(k, v) {
					continueProcessing = false
					return false
				}
				return true
			})

			if !continueProcessing {
				return
			}
		}
	}
}
