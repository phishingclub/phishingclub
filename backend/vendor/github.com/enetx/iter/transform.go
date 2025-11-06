package iter

// Map applies a function to each element, producing a new sequence of the same type.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3})
//	iter.Map(s, func(x int) int { return x * 2 }) // yields: 2, 4, 6
func Map[T any](s Seq[T], f func(T) T) Seq[T] {
	return func(yield func(T) bool) { s(func(v T) bool { return yield(f(v)) }) }
}

// MapTo applies a function to each element, producing a new sequence of potentially different type.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3})
//	iter.MapTo(s, func(x int) string { return fmt.Sprintf("%d", x) }) // yields: "1", "2", "3"
func MapTo[T, U any](s Seq[T], f func(T) U) Seq[U] {
	return func(yield func(U) bool) { s(func(v T) bool { return yield(f(v)) }) }
}

// Inspect applies a function to each element for side effects while passing through the original values.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3})
//	iter.Inspect(s, func(x int) { fmt.Printf("Processing: %d\n", x) })
func Inspect[T any](s Seq[T], fn func(T)) Seq[T] {
	return func(yield func(T) bool) { s(func(v T) bool { fn(v); return yield(v) }) }
}

// Filter returns a new sequence containing only elements that satisfy the predicate.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3, 4, 5})
//	iter.Filter(s, func(x int) bool { return x%2 == 0 }) // yields: 2, 4
func Filter[T any](s Seq[T], p func(T) bool) Seq[T] {
	return func(yield func(T) bool) {
		s(func(v T) bool {
			if p(v) {
				return yield(v)
			}
			return true
		})
	}
}

// Exclude returns a new sequence containing only elements that do not satisfy the predicate.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3, 4, 5})
//	iter.Exclude(s, func(x int) bool { return x%2 == 0 }) // yields: 1, 3, 5
func Exclude[T any](s Seq[T], p func(T) bool) Seq[T] {
	return func(yield func(T) bool) {
		s(func(v T) bool {
			if !p(v) {
				return yield(v)
			}
			return true
		})
	}
}

// FilterMap applies a function to each element and filters out None results.
//
// Example:
//
//	s := iter.FromSlice([]string{"1", "2", "abc", "3"})
//	iter.FilterMap(s, func(s string) (int, bool) {
//	  if i, err := strconv.Atoi(s); err == nil {
//	    return i, true
//	  }
//	  return 0, false
//	}) // yields: 1, 2, 3
func FilterMap[T, U any](s Seq[T], f func(T) (U, bool)) Seq[U] {
	return func(yield func(U) bool) {
		s(func(v T) bool {
			if u, ok := f(v); ok {
				return yield(u)
			}
			return true
		})
	}
}

// MapWhile applies a function to elements while it returns Some, stopping at the first None.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, -1, 4})
//	iter.MapWhile(s, func(x int) (int, bool) {
//	  if x > 0 { return x * 2, true }
//	  return 0, false
//	}) // yields: 2, 4
func MapWhile[T, U any](s Seq[T], f func(T) (U, bool)) Seq[U] {
	return func(yield func(U) bool) {
		s(func(v T) bool {
			if u, ok := f(v); ok {
				return yield(u)
			}
			return false
		})
	}
}

// Enumerate returns a sequence of (index, value) pairs starting from the given start index.
//
// Example:
//
//	s := iter.FromSlice([]string{"a", "b", "c"})
//	iter.Enumerate(s, 0) // yields: (0, "a"), (1, "b"), (2, "c")
func Enumerate[T any](s Seq[T], start int) Seq2[int, T] {
	return func(yield func(int, T) bool) {
		index := start
		s(func(v T) bool {
			result := yield(index, v)
			index++
			return result
		})
	}
}

// Scan is similar to Fold, but emits intermediate accumulator values.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3, 4})
//	iter.Scan(s, 0, func(acc, x int) int { return acc + x }) // yields: 1, 3, 6, 10
func Scan[T, S any](s Seq[T], init S, f func(S, T) S) Seq[S] {
	return func(yield func(S) bool) {
		acc := init
		s(func(v T) bool {
			acc = f(acc, v)
			return yield(acc)
		})
	}
}

// Unique returns a sequence with all duplicate elements removed.
// Works with any type by using any as the key.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 1, 2, 2, 3, 1})
//	iter.Unique(s) // yields: 1, 2, 3
func Unique[T any](s Seq[T]) Seq[T] {
	return func(yield func(T) bool) {
		seen := make(map[any]struct{})
		s(func(v T) bool {
			if _, exists := seen[any(v)]; !exists {
				seen[any(v)] = struct{}{}
				return yield(v)
			}
			return true
		})
	}
}

// UniqueBy returns a sequence with consecutive elements removed where the key function returns the same value.
//
// Example:
//
//	s := iter.FromSlice([]string{"aa", "bb", "a", "ccc"})
//	iter.UniqueBy(s, func(s string) int { return len(s) }) // yields: "aa", "a", "ccc"
func UniqueBy[T any, K comparable](s Seq[T], key func(T) K) Seq[T] {
	return func(yield func(T) bool) {
		var prevKey K
		first := true
		s(func(v T) bool {
			k := key(v)
			if first || k != prevKey {
				prevKey = k
				first = false
				return yield(v)
			}
			return true
		})
	}
}
