package iter

import "slices"

// Next2 extracts the first key-value pair from the sequence and returns the remaining sequence.
// Returns (key, value, remainingSeq, true) if a pair exists, or (zeroK, zeroV, nil, false) if empty.
// This is similar to Rust's Iterator::next() method for key-value pairs.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b", 3: "c"})
//	k, v, rest, ok := iter.Next2(s)
//	// k = 1, v = "a", ok = true (order not guaranteed for maps)
//	// rest yields remaining pairs
//
//	k2, v2, rest2, ok2 := iter.Next2(rest)
//	// k2 = 2, v2 = "b", ok2 = true
//	// rest2 yields remaining pairs
func Next2[K, V any](s Seq2[K, V]) (K, V, Seq2[K, V], bool) {
	var firstK K
	var firstV V
	found := false
	consumed := false

	s(func(k K, v V) bool {
		if !consumed {
			firstK = k
			firstV = v
			found = true
			consumed = true
			return false
		}
		return true
	})

	if !found {
		return firstK, firstV, nil, false
	}

	remaining := func(yield func(K, V) bool) {
		skip := true
		s(func(k K, v V) bool {
			if skip {
				skip = false
				return true
			}
			return yield(k, v)
		})
	}

	return firstK, firstV, remaining, true
}

// First2 returns the first key-value pair from the sequence.
// Returns (key, value, true) if a pair exists, or (zeroK, zeroV, false) if empty.
// This is similar to Rust's Iterator::first() method for key-value pairs.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b", 3: "c"})
//	k, v, ok := iter.First2(s) // might return: 1, "a", true
func First2[K, V any](s Seq2[K, V]) (K, V, bool) {
	var resultK K
	var resultV V
	found := false
	s(func(k K, v V) bool {
		resultK = k
		resultV = v
		found = true
		return false
	})
	return resultK, resultV, found
}

// Last2 returns the last key-value pair from the sequence.
// Returns (key, value, true) if a pair exists, or (zeroK, zeroV, false) if empty.
// This is similar to Rust's Iterator::last() method for key-value pairs.
//
// Example:
//
//	pairs := []Pair[int, string]{{1, "a"}, {2, "b"}, {3, "c"}}
//	s := iter.FromPairs(pairs)
//	k, v, ok := iter.Last2(s) // 3, "c", true
func Last2[K, V any](s Seq2[K, V]) (K, V, bool) {
	var resultK K
	var resultV V
	found := false
	s(func(k K, v V) bool {
		resultK = k
		resultV = v
		found = true
		return true
	})
	return resultK, resultV, found
}

// ForEach2 applies a function to each key-value pair in the sequence.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	iter.ForEach2(s, func(k int, v string) { fmt.Printf("%d: %s\n", k, v) })
func ForEach2[K, V any](s Seq2[K, V], fn func(K, V)) {
	s(func(k K, v V) bool { fn(k, v); return true })
}

// Count2 returns the number of key-value pairs in the sequence.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	count := iter.Count2(s) // 2
func Count2[K, V any](s Seq2[K, V]) int {
	count := 0
	s(func(K, V) bool { count++; return true })
	return count
}

// Range2 applies a function to each key-value pair until it returns false.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b", 3: "c"})
//	iter.Range2(s, func(k int, v string) bool {
//	  fmt.Printf("%d: %s\n", k, v)
//	  return k != 2 // Stop at key 2
//	})
func Range2[K, V any](s Seq2[K, V], fn func(K, V) bool) { s(fn) }

// Map2 applies a function to each key-value pair, producing a new Seq2.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	iter.Map2(s, func(k int, v string) (int, string) {
//	  return k*10, strings.ToUpper(v)
//	}) // yields: (10, "A"), (20, "B")
func Map2[K, V, K2, V2 any](s Seq2[K, V], f func(K, V) (K2, V2)) Seq2[K2, V2] {
	return func(yield func(K2, V2) bool) {
		s(func(k K, v V) bool {
			k2, v2 := f(k, v)
			return yield(k2, v2)
		})
	}
}

// Filter2 returns a new Seq2 containing only pairs that satisfy the predicate.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "bb", 3: "ccc"})
//	iter.Filter2(s, func(k int, v string) bool { return len(v) > 1 })
//	// yields pairs where value length > 1
func Filter2[K, V any](s Seq2[K, V], p func(K, V) bool) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		s(func(k K, v V) bool {
			if p(k, v) {
				return yield(k, v)
			}
			return true
		})
	}
}

// Exclude2 returns a new Seq2 containing only pairs that do not satisfy the predicate.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "bb", 3: "ccc"})
//	iter.Exclude2(s, func(k int, v string) bool { return len(v) > 1 })
//	// yields pairs where value length <= 1
func Exclude2[K, V any](s Seq2[K, V], p func(K, V) bool) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		s(func(k K, v V) bool {
			if !p(k, v) {
				return yield(k, v)
			}
			return true
		})
	}
}

// FilterMap2 applies a function to each key-value pair and filters out None results.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "bb", 3: "ccc"})
//	iter.FilterMap2(s, func(k int, v string) (Pair[int, string], bool) {
//	  if len(v) > 1 {
//	    return Pair[int, string]{k*10, strings.ToUpper(v)}, true
//	  }
//	  return Pair[int, string]{}, false
//	}) // yields: (20, "BB"), (30, "CCC")
func FilterMap2[K, V, K2, V2 any](s Seq2[K, V], f func(K, V) (Pair[K2, V2], bool)) Seq2[K2, V2] {
	return func(yield func(K2, V2) bool) {
		s(func(k K, v V) bool {
			if pair, ok := f(k, v); ok {
				return yield(pair.Key, pair.Value)
			}
			return true
		})
	}
}

// Find2 returns the first key-value pair that satisfies the predicate.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "bb", 3: "ccc"})
//	k, v, ok := iter.Find2(s, func(k int, v string) bool { return len(v) > 1 })
//	// might return: 2, "bb", true
func Find2[K, V any](s Seq2[K, V], p func(K, V) bool) (K, V, bool) {
	var resultK K
	var resultV V
	found := false
	s(func(k K, v V) bool {
		if p(k, v) {
			resultK = k
			resultV = v
			found = true
			return false
		}
		return true
	})
	return resultK, resultV, found
}

// Keys extracts all keys from the Seq2 into a Seq.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	iter.Keys(s) // yields: 1, 2 (order not guaranteed)
func Keys[K, V any](s Seq2[K, V]) Seq[K] {
	return func(y func(K) bool) { s(func(k K, _ V) bool { return y(k) }) }
}

// Values extracts all values from the Seq2 into a Seq.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	iter.Values(s) // yields: "a", "b" (order not guaranteed)
func Values[K, V any](s Seq2[K, V]) Seq[V] {
	return func(y func(V) bool) { s(func(_ K, v V) bool { return y(v) }) }
}

// OrderByKey sorts the sequence by keys using the comparison function.
func OrderByKey[K, V any](s Seq2[K, V], less func(a, b K) bool) Seq2[K, V] {
	buf := ToPairs(s)
	slices.SortFunc(buf, func(a, b Pair[K, V]) int {
		switch {
		case less(a.Key, b.Key):
			return -1
		case less(b.Key, a.Key):
			return 1
		default:
			return 0
		}
	})
	return FromPairs(buf)
}

// OrderByValue sorts the sequence by values using the comparison function.
func OrderByValue[K, V any](s Seq2[K, V], less func(a, b V) bool) Seq2[K, V] {
	buf := ToPairs(s)
	slices.SortFunc(buf, func(a, b Pair[K, V]) int {
		switch {
		case less(a.Value, b.Value):
			return -1
		case less(b.Value, a.Value):
			return 1
		default:
			return 0
		}
	})
	return FromPairs(buf)
}

// Take2 returns the first n key-value pairs of the sequence.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b", 3: "c"})
//	iter.Take2(s, 2) // yields first 2 pairs
func Take2[K, V any](s Seq2[K, V], n int) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if n <= 0 {
			return
		}
		count := 0
		s(func(k K, v V) bool {
			if count >= n {
				return false
			}
			count++
			return yield(k, v)
		})
	}
}

// Skip2 skips the first n key-value pairs and returns the rest.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b", 3: "c"})
//	iter.Skip2(s, 1) // yields all pairs except the first
func Skip2[K, V any](s Seq2[K, V], n int) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if n <= 0 {
			s(yield)
			return
		}
		count := 0
		s(func(k K, v V) bool {
			if count < n {
				count++
				return true
			}
			return yield(k, v)
		})
	}
}

// StepBy2 returns every nth key-value pair from the sequence.
//
// Example:
//
//	pairs := []Pair[int, string]{{1, "a"}, {2, "b"}, {3, "c"}, {4, "d"}}
//	s := iter.FromPairs(pairs)
//	iter.StepBy2(s, 2) // yields: (1, "a"), (3, "c")
func StepBy2[K, V any](s Seq2[K, V], step int) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if step <= 0 {
			return
		}
		index := 0
		s(func(k K, v V) bool {
			if index%step == 0 {
				if !yield(k, v) {
					return false
				}
			}
			index++
			return true
		})
	}
}

// SortBy2 sorts the sequence using the provided comparison function on Pair pairs.
func SortBy2[K, V any](s Seq2[K, V], less func(a, b Pair[K, V]) bool) Seq2[K, V] {
	buf := ToPairs(s)
	slices.SortFunc(buf, func(a, b Pair[K, V]) int {
		if less(a, b) {
			return -1
		}
		if less(b, a) {
			return 1
		}
		return 0
	})
	return FromPairs(buf)
}

// Inspect2 applies a function to each key-value pair for side effects while passing through the original pairs.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	iter.Inspect2(s, func(k int, v string) { fmt.Printf("Processing: %d=%s\n", k, v) })
func Inspect2[K, V any](s Seq2[K, V], fn func(K, V)) Seq2[K, V] {
	return func(yield func(K, V) bool) { s(func(k K, v V) bool { fn(k, v); return yield(k, v) }) }
}

// Any2 returns true if any key-value pair satisfies the predicate.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "bb"})
//	hasLongValue := iter.Any2(s, func(k int, v string) bool { return len(v) > 1 }) // true
func Any2[K, V any](s Seq2[K, V], p func(K, V) bool) bool {
	found := false
	s(func(k K, v V) bool {
		if p(k, v) {
			found = true
			return false
		}
		return true
	})
	return found
}

// All2 returns true if all key-value pairs satisfy the predicate.
//
// Example:
//
//	s := iter.FromMap(map[int]string{1: "a", 2: "b"})
//	allShortValues := iter.All2(s, func(k int, v string) bool { return len(v) == 1 }) // true
func All2[K, V any](s Seq2[K, V], p func(K, V) bool) bool {
	all := true
	s(func(k K, v V) bool {
		if !p(k, v) {
			all = false
			return false
		}
		return true
	})
	return all
}

// Fold2 reduces the Seq2 to a single value using an accumulator.
//
// Example:
//
//	s := iter.FromMap(map[int]int{1: 10, 2: 20, 3: 30})
//	sum := iter.Fold2(s, 0, func(acc, k, v int) int { return acc + k + v }) // 66
func Fold2[K, V, A any](s Seq2[K, V], acc A, f func(A, K, V) A) A {
	s(func(k K, v V) bool { acc = f(acc, k, v); return true })
	return acc
}

// Reduce2 reduces the Seq2 to a single Pair pair.
// Returns false if the sequence is empty.
//
// Example:
//
//	s := iter.FromMap(map[int]int{1: 10, 2: 20})
//	result, ok := iter.Reduce2(s, func(a, b Pair[int, int]) Pair[int, int] {
//	  return Pair[int, int]{a.Key + b.Key, a.Value + b.Value}
//	}) // Pair{3, 30}, true
func Reduce2[K, V any](s Seq2[K, V], f func(Pair[K, V], Pair[K, V]) Pair[K, V]) (Pair[K, V], bool) {
	var result Pair[K, V]
	first := true
	s(func(k K, v V) bool {
		kv := Pair[K, V]{k, v}
		if first {
			result = kv
			first = false
		} else {
			result = f(result, kv)
		}
		return true
	})
	return result, !first
}

// Nth2 returns the nth key-value pair (0-indexed) from the sequence.
//
// Example:
//
//	pairs := []Pair[int, string]{{1, "a"}, {2, "b"}, {3, "c"}}
//	s := iter.FromPairs(pairs)
//	k, v, ok := iter.Nth2(s, 1) // 2, "b", true
func Nth2[K, V any](s Seq2[K, V], n int) (K, V, bool) {
	var resultK K
	var resultV V
	found := false
	index := 0
	s(func(k K, v V) bool {
		if index == n {
			resultK = k
			resultV = v
			found = true
			return false
		}
		index++
		return true
	})
	return resultK, resultV, found
}
