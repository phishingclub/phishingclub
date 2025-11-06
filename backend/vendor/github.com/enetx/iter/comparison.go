package iter

import "reflect"

// Cmp compares two sequences lexicographically using the provided comparison function.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{1, 2, 4})
//	result := iter.Cmp(s1, s2, func(a, b int) int {
//	  if a < b { return -1 }
//	  if a > b { return 1 }
//	  return 0
//	}) // -1
func Cmp[T any](a, b Seq[T], cmp func(T, T) int) int {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if !aok && !bok {
			return 0
		}
		if !aok {
			return -1
		}
		if !bok {
			return 1
		}

		if c := cmp(av, bv); c != 0 {
			return c
		}
	}
}

// Equal checks if two sequences are equal (same elements in same order).
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{1, 2, 3})
//	equal := iter.Equal(s1, s2) // true
func Equal[T any](a, b Seq[T]) bool {
	var zero T
	if reflect.ValueOf(zero).Comparable() {
		return EqualBy(a, b, func(x, y T) bool { return any(x) == any(y) })
	}
	return EqualBy(a, b, func(x, y T) bool { return reflect.DeepEqual(x, y) })
}

func EqualBy[T any](a, b Seq[T], eq func(T, T) bool) bool {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if aok != bok {
			return false
		}
		if !aok {
			return true
		}
		if !eq(av, bv) {
			return false
		}
	}
}

// Lt checks if sequence a is lexicographically less than sequence b.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{1, 2, 4})
//	isLess := iter.Lt(s1, s2, func(a, b int) bool { return a < b }) // true
func Lt[T any](a, b Seq[T], less func(T, T) bool) bool {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if !aok && bok {
			return true
		}
		if !aok || !bok {
			return false
		}

		if less(av, bv) {
			return true
		}
		if less(bv, av) {
			return false
		}
	}
}

// Le checks if sequence a is lexicographically less than or equal to sequence b.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{1, 2, 3})
//	isLessOrEqual := iter.Le(s1, s2, func(a, b int) bool { return a < b }) // true
func Le[T any](a, b Seq[T], less func(T, T) bool) bool {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if !aok {
			return true
		}
		if !bok {
			return false
		}

		if less(av, bv) {
			return true
		}
		if less(bv, av) {
			return false
		}
	}
}

// Gt checks if sequence a is lexicographically greater than sequence b.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 4})
//	s2 := iter.FromSlice([]int{1, 2, 3})
//	isGreater := iter.Gt(s1, s2, func(a, b int) bool { return a < b }) // true
func Gt[T any](a, b Seq[T], less func(T, T) bool) bool {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if !bok && aok {
			return true
		}
		if !aok || !bok {
			return false
		}

		if less(bv, av) {
			return true
		}
		if less(av, bv) {
			return false
		}
	}
}

// Ge checks if sequence a is lexicographically greater than or equal to sequence b.
//
// Example:
//
//	s1 := iter.FromSlice([]int{1, 2, 3})
//	s2 := iter.FromSlice([]int{1, 2, 3})
//	isGreaterOrEqual := iter.Ge(s1, s2, func(a, b int) bool { return a < b }) // true
func Ge[T any](a, b Seq[T], less func(T, T) bool) bool {
	an, as := Pull(a)
	defer as()
	bn, bs := Pull(b)
	defer bs()

	for {
		av, aok := an()
		bv, bok := bn()

		if !bok {
			return true
		}
		if !aok {
			return false
		}

		if less(bv, av) {
			return true
		}
		if less(av, bv) {
			return false
		}
	}
}
