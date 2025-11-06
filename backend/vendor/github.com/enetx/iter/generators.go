package iter

// Iota generates a sequence of numbers from start (inclusive) to stop (exclusive) with the given step.
// If no step is provided, defaults to 1.
//
// Example:
//
//	iter.Iota(1, 5)    // yields: 1, 2, 3, 4
//	iter.Iota(1, 10, 2) // yields: 1, 3, 5, 7, 9
func Iota[T Integer](start, stop T, step ...T) Seq[T] {
	stepValue := T(1)
	if len(step) > 0 {
		stepValue = step[0]
	}

	return func(yield func(T) bool) {
		if stepValue == 0 {
			return
		}

		if stepValue > 0 {
			for i := start; i < stop; i += stepValue {
				if !yield(i) {
					return
				}
			}
		} else {
			for i := start; i > stop; i += stepValue {
				if !yield(i) {
					return
				}
			}
		}
	}
}

// IotaInclusive generates a sequence of numbers from start to stop (both inclusive) with the given step.
// If no step is provided, defaults to 1.
//
// Example:
//
//	iter.IotaInclusive(1, 5)    // yields: 1, 2, 3, 4, 5
//	iter.IotaInclusive(1, 9, 2) // yields: 1, 3, 5, 7, 9
func IotaInclusive[T Integer](start, stop T, step ...T) Seq[T] {
	stepValue := T(1)
	if len(step) > 0 {
		stepValue = step[0]
	}

	return func(yield func(T) bool) {
		if stepValue == 0 {
			return
		}

		if stepValue > 0 {
			for i := start; i <= stop; i += stepValue {
				if !yield(i) {
					return
				}
			}
		} else {
			for i := start; i >= stop; i += stepValue {
				if !yield(i) {
					return
				}
			}
		}
	}
}

// Once creates a sequence that yields a single value.
//
// Example:
//
//	s := iter.Once(42) // yields: 42
func Once[T any](value T) Seq[T] {
	return func(yield func(T) bool) {
		yield(value)
	}
}

// OnceWith creates a sequence that yields a single value from a function.
//
// Example:
//
//	s := iter.OnceWith(func() int { return rand.Int() })
func OnceWith[T any](f func() T) Seq[T] {
	return func(yield func(T) bool) {
		yield(f())
	}
}

// Empty creates an empty sequence.
//
// Example:
//
//	s := iter.Empty[int]() // yields nothing
func Empty[T any]() Seq[T] { return func(func(T) bool) {} }

// Repeat creates an infinite sequence that repeats the given value.
//
// Example:
//
//	s := iter.Take(iter.Repeat(42), 3) // yields: 42, 42, 42
func Repeat[T any](value T) Seq[T] {
	return func(yield func(T) bool) {
		for {
			if !yield(value) {
				return
			}
		}
	}
}

// RepeatWith creates an infinite sequence by repeatedly calling a function.
//
// Example:
//
//	s := iter.Take(iter.RepeatWith(func() int { return rand.Int() }), 3)
func RepeatWith[T any](f func() T) Seq[T] {
	return func(yield func(T) bool) {
		for {
			if !yield(f()) {
				return
			}
		}
	}
}
