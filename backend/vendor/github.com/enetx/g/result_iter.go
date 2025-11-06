package g

import (
	"context"

	"github.com/enetx/g/f"
	"github.com/enetx/iter"
)

// Pull converts the “push-style” sequence of Result[V] into a “pull-style” iterator accessed by two functions: next and stop.
//
// The next function returns the next Result[V] in the sequence and a boolean indicating whether the value is valid.
// When the sequence is over, next returns the zero value and false. It is valid to call next after reaching the end
// of the sequence or after calling stop. These calls will continue to return the zero value and false.
//
// The stop function ends the iteration. It must be called when the caller is no longer interested in next values and
// next has not yet signaled that the sequence is over. It is valid to call stop multiple times and after next has
// already returned false.
//
// It is an error to call next or stop from multiple goroutines simultaneously.
func (seq SeqResult[V]) Pull() (func() (Result[V], bool), func()) {
	return iter.Pull(iter.Seq[Result[V]](seq))
}

// All checks whether all Ok values in the sequence satisfy the provided condition.
//
// If an Err is encountered in the sequence, that Err is immediately returned.
// Otherwise, it returns Ok(true) if all Ok values satisfy the function, or Ok(false) if at least one does not.
func (seq SeqResult[V]) All(fn func(v V) bool) Result[bool] {
	result := Ok(true)

	seq(func(v Result[V]) bool {
		if v.IsErr() {
			result = Err[bool](v.err)
			return false
		}

		if !fn(v.v) {
			result = Ok(false)
			return false
		}

		return true
	})

	return result
}

// Any checks whether any Ok value in the sequence satisfies the provided condition.
//
// If an Err is encountered, that Err is immediately returned.
// Otherwise, it returns Ok(true) if at least one Ok value satisfies the function, or Ok(false) if none do.
func (seq SeqResult[V]) Any(fn func(v V) bool) Result[bool] {
	result := Ok(false)

	seq(func(v Result[V]) bool {
		if v.IsErr() {
			result = Err[bool](v.err)
			return false
		}

		if fn(v.v) {
			result = Ok(true)
			return false
		}

		return true
	})

	return result
}

// Collect gathers all Ok values from the iterator into a Slice.
// If any value is Err, the first such Err is returned immediately.
func (seq SeqResult[V]) Collect() Slice[Result[V]] { return iter.ToSlice(iter.Seq[Result[V]](seq)) }

// Count consumes the entire sequence, counting how many times the yield function is invoked.
// Err elements do not stop the count but are still passed to the yield function (which returns false immediately, stopping iteration).
func (seq SeqResult[V]) Count() Int {
	var counter Int
	seq(func(Result[V]) bool {
		counter++
		return true
	})

	return counter
}

// Map transforms each Ok value in the sequence using the given function, returning a new sequence of Result.
//
// If an Err is encountered, it is passed downstream as-is and ends the iteration (yield returns false).
func (seq SeqResult[V]) Map(transform func(V) V) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			return yield(Ok(transform(v.v)))
		})
	}
}

// Filter returns a new sequence containing only the Ok elements that satisfy the provided function.
//
// If an Err is encountered, it is yielded immediately as Err (and stops further iteration).
// Only Ok elements for which fn returns true are yielded downstream as Ok.
func (seq SeqResult[V]) Filter(fn func(V) bool) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			if fn(v.v) {
				return yield(v)
			}
			return true
		})
	}
}

// Exclude returns a new sequence that excludes Ok elements which satisfy the provided function.
//
// If an Err is encountered, it is yielded as Err (and stops iteration).
// Only Ok elements for which 'fn' returns false are yielded downstream.
func (seq SeqResult[V]) Exclude(fn func(V) bool) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			if !fn(v.v) {
				return yield(v)
			}
			return true
		})
	}
}

// Dedup removes consecutive duplicates of Ok values from the sequence, returning a new sequence.
//
// If an Err is encountered, it is yielded immediately and iteration stops.
// Consecutive Ok duplicates (based on equality) are filtered out so only the first occurrence is yielded.
func (seq SeqResult[V]) Dedup() SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		var current V
		hasFirst := false
		comparable := f.IsComparable(current)

		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}

			if !hasFirst {
				hasFirst = true
				current = v.v
				return yield(v)
			}

			if comparable {
				if f.Eq[any](current)(v.v) {
					return true
				}
			} else {
				if f.Eqd(current)(v.v) {
					return true
				}
			}

			current = v.v
			return yield(v)
		})
	}
}

// Unique returns a new sequence that contains only the first occurrence of each distinct Ok value.
//
// If an Err is encountered, it is yielded immediately and iteration stops.
// Future occurrences of a previously seen Ok value are skipped.
func (seq SeqResult[V]) Unique() SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seen := NewSet[any]()
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			if !seen.Contains(v.v) {
				seen.Insert(v.v)
				return yield(v)
			}
			return true
		})
	}
}

// ForEach applies a function to each Result in the sequence (Ok or Err) without modifying the sequence.
//
// The iteration continues over all elements, passing them to fn for side effects.
func (seq SeqResult[V]) ForEach(fn func(v Result[V])) {
	seq(func(v Result[V]) bool {
		fn(v)
		return true
	})
}

// Range iterates through elements until the given function returns false.
//
// For each element (Ok or Err), fn is called. If fn returns false, iteration stops immediately.
func (seq SeqResult[V]) Range(fn func(v Result[V]) bool) {
	seq(func(v Result[V]) bool {
		return fn(v)
	})
}

// Skip returns a new sequence that skips the first n Ok elements.
//
// If an Err is encountered, it is yielded as is and iteration stops. Once n Ok elements have been skipped,
// subsequent elements (Ok or Err) are yielded normally.
func (seq SeqResult[V]) Skip(n uint) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			if n > 0 {
				n--
				return true
			}
			return yield(v)
		})
	}
}

// StepBy creates a new sequence that yields every nth Ok element from the original sequence.
//
// If an Err is encountered, it is yielded immediately and stops iteration.
// For Ok elements, only every n-th element is yielded.
func (seq SeqResult[V]) StepBy(n uint) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		i := uint(0)
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			i++
			if (i-1)%n == 0 {
				return yield(v)
			}
			return true
		})
	}
}

// Take returns a new sequence with the first n Ok elements.
// If an Err is encountered, it is yielded immediately and iteration stops.
// After n Ok elements are yielded, the sequence ends.
func (seq SeqResult[V]) Take(n uint) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			if n == 0 {
				return false
			}
			n--
			return yield(v)
		})
	}
}

// Nth returns the nth Ok element (0-indexed) in the sequence.
// If an Err is encountered before reaching the nth element, that Err is returned.
// If there are fewer than n+1 Ok elements, None is returned.
func (seq SeqResult[V]) Nth(n Int) Result[Option[V]] {
	if n < 0 {
		return Ok(None[V]())
	}

	var i Int
	result := Ok(None[V]())
	found := false

	seq(func(v Result[V]) bool {
		if found {
			return false
		}
		if v.IsErr() {
			result = Err[Option[V]](v.err)
			found = true
			return false
		}
		if i == n {
			result = Ok(Some(v.v))
			found = true
			return false
		}
		i++
		return true
	})

	return result
}

// Chain concatenates this sequence with other sequences, returning a new sequence of Result[V].
//
// The function yields all elements (Ok or Err) from the current sequence, then from each of the provided sequences in order.
// If an Err is encountered, it is yielded immediately, ending further iteration.
func (seq SeqResult[V]) Chain(seqs ...SeqResult[V]) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		for _, seq := range append([]SeqResult[V]{seq}, seqs...) {
			seq(func(v Result[V]) bool {
				if v.IsErr() {
					yield(v)
					return false
				}
				return yield(v)
			})
		}
	}
}

// Intersperse inserts the provided Ok separator between each Ok element of the sequence.
//
// If an Err is encountered, it is yielded as Err and iteration stops immediately.
// For Ok elements, after the first yield, a separator is inserted before each subsequent Ok value.
func (seq SeqResult[V]) Intersperse(sep V) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		first := true

		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}

			if !first && !yield(Ok(sep)) {
				return false
			}

			first = false
			return yield(v)
		})
	}
}

// Inspect calls fn for every Ok value without changing it.
// An Err immediately stops iteration by returning false.
func (seq SeqResult[V]) Inspect(fn func(v V)) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				yield(v)
				return false
			}
			fn(v.v)
			return yield(v)
		})
	}
}

// Find searches the sequence for the first Ok value that satisfies the provided function.
//
// If an Err is encountered, it returns that Err immediately. If a matching Ok value is found,
// iteration stops and we return Ok(Some(...)). If no matching Ok value is found, it returns Ok(None).
func (seq SeqResult[V]) Find(fn func(V) bool) Result[Option[V]] {
	result := Ok(None[V]())

	seq(func(v Result[V]) bool {
		if v.IsErr() {
			result = Err[Option[V]](v.err)
			return false
		}
		if fn(v.v) {
			result = Ok(Some(v.v))
			return false
		}
		return true
	})

	return result
}

// Context allows the iteration to be controlled with a context.Context.
func (seq SeqResult[V]) Context(ctx context.Context) SeqResult[V] {
	return func(yield func(Result[V]) bool) {
		seq(func(v Result[V]) bool {
			select {
			case <-ctx.Done():
				return false
			default:
				return yield(v)
			}
		})
	}
}

// First returns the first Ok element from the sequence.
// If the sequence is empty or contains only Err values, None is returned.
// If an Err is encountered, that Err is returned.
func (seq SeqResult[V]) First() Result[Option[V]] {
	result := Ok(None[V]())
	found := false

	seq(func(v Result[V]) bool {
		if found {
			return false
		}
		if v.IsErr() {
			result = Err[Option[V]](v.err)
			found = true
			return false
		}
		result = Ok(Some(v.v))
		found = true
		return false
	})

	return result
}

// Last returns the last Ok element from the sequence.
// If the sequence is empty or contains only Err values, None is returned.
// If an Err is encountered, that Err is returned.
func (seq SeqResult[V]) Last() Result[Option[V]] {
	result := Ok(None[V]())

	seq(func(v Result[V]) bool {
		if v.IsErr() {
			result = Err[Option[V]](v.err)
			return false
		}
		result = Ok(Some(v.v))
		return true
	})

	return result
}

// Next extracts the next element from the iterator and advances it.
//
// This method consumes the next element from the iterator and returns it wrapped in an Option.
// The iterator itself is modified to point to the remaining elements.
//
// Returns:
// - Option[Result[V]]: Some(Result[V]) if an element exists, None if the iterator is exhausted.
func (seq *SeqResult[V]) Next() Option[Result[V]] {
	if value, remaining, ok := iter.Next(iter.Seq[Result[V]](*seq)); ok {
		*seq = SeqResult[V](remaining)
		return Some(value)
	}

	return None[Result[V]]()
}

// Partition separates the sequence into two slices: one containing all Ok values and one containing all errors.
// The iteration continues through all elements, collecting each into the appropriate slice.
func (seq SeqResult[V]) Partition() (Slice[V], Slice[error]) {
	ok := NewSlice[V]()
	err := NewSlice[error]()

	seq(func(v Result[V]) bool {
		if v.IsOk() {
			ok = append(ok, v.v)
		} else {
			err = append(err, v.err)
		}
		return true
	})

	return ok, err
}

// Ok returns a new sequence containing only the Ok values from the original sequence.
// All Err values are filtered out.
func (seq SeqResult[V]) Ok() SeqSlice[V] {
	return SeqSlice[V](func(yield func(V) bool) {
		seq(func(v Result[V]) bool {
			if v.IsOk() {
				return yield(v.v)
			}
			return true
		})
	})
}

// Err returns a new sequence containing only the error values from the original sequence.
// All Ok values are filtered out.
func (seq SeqResult[V]) Err() SeqSlice[error] {
	return SeqSlice[error](func(yield func(error) bool) {
		seq(func(v Result[V]) bool {
			if v.IsErr() {
				return yield(v.err)
			}
			return true
		})
	})
}

// FirstErr returns the first error encountered in the sequence.
// If no error is found, it returns None. The iteration stops at the first error.
func (seq SeqResult[V]) FirstErr() Option[error] {
	result := None[error]()
	found := false

	seq(func(v Result[V]) bool {
		if found {
			return false
		}
		if v.IsErr() {
			result = Some(v.err)
			found = true
			return false
		}
		return true
	})

	return result
}
