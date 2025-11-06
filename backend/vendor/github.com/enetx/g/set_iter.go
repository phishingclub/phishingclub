package g

import (
	"context"

	"github.com/enetx/iter"
)

// Pull converts the “push-style” iterator sequence seq
// into a “pull-style” iterator accessed by the two functions
// next and stop.
//
// Next returns the next value in the sequence
// and a boolean indicating whether the value is valid.
// When the sequence is over, next returns the zero V and false.
// It is valid to call next after reaching the end of the sequence
// or after calling stop. These calls will continue
// to return the zero V and false.
//
// Stop ends the iteration. It must be called when the caller is
// no longer interested in next values and next has not yet
// signaled that the sequence is over (with a false boolean return).
// It is valid to call stop multiple times and when next has
// already returned false.
//
// It is an error to call next or stop from multiple goroutines
// simultaneously.
func (seq SeqSet[V]) Pull() (func() (V, bool), func()) { return iter.Pull(iter.Seq[V](seq)) }

// Inspect creates a new iterator that wraps around the current iterator
// and allows inspecting each element as it passes through.
func (seq SeqSet[V]) Inspect(fn func(v V)) SeqSet[V] {
	return SeqSet[V](iter.Inspect(iter.Seq[V](seq), fn))
}

// Collect gathers all elements from the iterator into a Set.
func (seq SeqSet[V]) Collect() Set[V] {
	collection := make(Set[V])

	seq(func(v V) bool {
		collection[v] = struct{}{}
		return true
	})

	return collection
}

// Chain concatenates the current iterator with other iterators, returning a new iterator.
//
// The function creates a new iterator that combines the elements of the current iterator
// with elements from the provided iterators in the order they are given.
//
// Params:
//
// - seqs ([]SeqSet[V]): Other iterators to be concatenated with the current iterator.
//
// Returns:
//
// - SeqSet[V]: A new iterator containing elements from the current iterator and the provided iterators.
//
// Example usage:
//
//	iter1 := g.SetOf(1, 2, 3).Iter()
//	iter2 := g.SetOf(4, 5, 6).Iter()
//	iter1.Chain(iter2).Collect().Print()
//
// Output: Set{3, 4, 5, 6, 1, 2} // The output order may vary as the Set type is not ordered.
//
// The resulting iterator will contain elements from both iterators.
func (seq SeqSet[V]) Chain(seqs ...SeqSet[V]) SeqSet[V] {
	iterSeqs := make([]iter.Seq[V], len(seqs))
	for i, s := range seqs {
		iterSeqs[i] = iter.Seq[V](s)
	}

	return SeqSet[V](iter.Chain(iter.Seq[V](seq), iterSeqs...))
}

// Count consumes the iterator, counting the number of iterations and returning it.
func (seq SeqSet[V]) Count() Int { return Int(iter.Count(iter.Seq[V](seq))) }

// ForEach iterates through all elements and applies the given function to each.
//
// The function applies the provided function to each element of the iterator.
//
// Params:
//
// - fn (func(V)): The function to apply to each element.
//
// Example usage:
//
//	iter := g.SetOf(1, 2, 3).Iter()
//	iter.ForEach(func(val V) {
//	    fmt.Println(val) // Replace this with the function logic you need.
//	})
//
// The provided function will be applied to each element in the iterator.
func (seq SeqSet[V]) ForEach(fn func(v V)) { iter.ForEach(iter.Seq[V](seq), fn) }

// Range iterates through elements until the given function returns false.
//
// The function iterates through the elements of the iterator and applies the provided function to each element.
// The iteration will stop when the provided function returns false for an element.
//
// Params:
// - fn (func(V) bool): The function that evaluates elements for continuation of iteration.
//
// Example usage:
//
// iter := g.SetOf(1, 2, 2, 3, 4, 5).Iter()
//
//	iter.Range(func(v int) bool {
//	    if v == 3 {
//	        return false
//	    }
//	    print(v)
//	    return true
//	})
func (seq SeqSet[V]) Range(fn func(v V) bool) { iter.Range(iter.Seq[V](seq), fn) }

// Filter returns a new iterator containing only the elements that satisfy the provided function.
//
// The function applies the provided function to each element of the iterator.
// If the function returns true for an element, that element is included in the resulting iterator.
//
// Parameters:
//
// - fn (func(V) bool): The function to be applied to each element of the iterator
// to determine if it should be included in the result.
//
// Returns:
//
// - SeqSet[V]: A new iterator containing the elements that satisfy the given condition.
//
// Example usage:
//
//	set := g.SetOf(1, 2, 3, 4, 5)
//	even := set.Iter().
//		Filter(
//			func(val int) bool {
//				return val%2 == 0
//			}).
//		Collect()
//	even.Print()
//
// Output: Set{2, 4} // The output order may vary as the Set type is not ordered.
//
// The resulting iterator will contain only the elements that satisfy the provided function.
func (seq SeqSet[V]) Filter(fn func(V) bool) SeqSet[V] {
	return SeqSet[V](iter.Filter(iter.Seq[V](seq), fn))
}

// Exclude returns a new iterator excluding elements that satisfy the provided function.
//
// The function applies the provided function to each element of the iterator.
// If the function returns true for an element, that element is excluded from the resulting iterator.
//
// Parameters:
//
// - fn (func(V) bool): The function to be applied to each element of the iterator
// to determine if it should be excluded from the result.
//
// Returns:
//
// - SeqSet[V]: A new iterator containing the elements that do not satisfy the given condition.
//
// Example usage:
//
//	set := g.SetOf(1, 2, 3, 4, 5)
//	notEven := set.Iter().
//		Exclude(
//			func(val int) bool {
//				return val%2 == 0
//			}).
//		Collect()
//	notEven.Print()
//
// Output: Set{1, 3, 5} // The output order may vary as the Set type is not ordered.
//
// The resulting iterator will contain only the elements that do not satisfy the provided function.
func (seq SeqSet[V]) Exclude(fn func(V) bool) SeqSet[V] {
	return SeqSet[V](iter.Exclude(iter.Seq[V](seq), fn))
}

// Map transforms each element in the iterator using the given function.
//
// The function creates a new iterator by applying the provided function to each element
// of the original iterator.
//
// Params:
//
// - fn (func(V) V): The function used to transform elements.
//
// Returns:
//
// - SeqSet[V]: A new iterator containing elements transformed by the provided function.
//
// Example usage:
//
//	set := g.SetOf(1, 2, 3)
//	doubled := set.Iter().
//		Map(
//			func(val int) int {
//				return val * 2
//			}).
//		Collect()
//	doubled.Print()
//
// Output: Set{2, 4, 6} // The output order may vary as the Set type is not ordered.
//
// The resulting iterator will contain elements transformed by the provided function.
func (seq SeqSet[V]) Map(transform func(V) V) SeqSet[V] {
	return SeqSet[V](iter.Map(iter.Seq[V](seq), transform))
}

// Find searches for an element in the iterator that satisfies the provided function.
//
// The function iterates through the elements of the iterator and returns the first element
// for which the provided function returns true.
//
// Params:
//
// - fn (func(V) bool): The function used to test elements for a condition.
//
// Returns:
//
// - Option[V]: An Option containing the first element that satisfies the condition; None if not found.
//
// Example usage:
//
//	iter := g.SetOf(1, 2, 3, 4, 5).Iter()
//
//	found := iter.Find(
//		func(i int) bool {
//			return i == 2
//		})
//
//	if found.IsSome() {
//		fmt.Println("Found:", found.Some())
//	} else {
//		fmt.Println("Not found.")
//	}
//
// The resulting Option may contain the first element that satisfies the condition, or None if not found.
func (seq SeqSet[V]) Find(fn func(v V) bool) Option[V] {
	return OptionOf(iter.Find(iter.Seq[V](seq), fn))
}

// Context allows the iteration to be controlled with a context.Context.
func (seq SeqSet[V]) Context(ctx context.Context) SeqSet[V] {
	return SeqSet[V](iter.Context(iter.Seq[V](seq), ctx))
}

// Take returns a new iterator with the first n elements.
// The function creates a new iterator containing the first n elements from the original iterator.
func (seq SeqSet[V]) Take(n uint) SeqSet[V] { return SeqSet[V](iter.Take(iter.Seq[V](seq), int(n))) }

// Nth returns the nth element (0-indexed) in the sequence.
func (seq SeqSet[V]) Nth(n Int) Option[V] {
	return OptionOf(iter.Nth(iter.Seq[V](seq), int(n)))
}

func difference[V comparable](seq SeqSet[V], other Set[V]) SeqSet[V] {
	return func(yield func(V) bool) {
		seq(func(v V) bool {
			if !other.Contains(v) {
				return yield(v)
			}
			return true
		})
	}
}

// Next extracts the next element from the iterator and advances it.
//
// This method consumes the next element from the iterator and returns it wrapped in an Option.
// The iterator itself is modified to point to the remaining elements.
//
// Returns:
// - Option[V]: Some(value) if an element exists, None if the iterator is exhausted.
func (seq *SeqSet[V]) Next() Option[V] {
	var values []V

	(*seq)(func(v V) bool {
		values = append(values, v)
		return true
	})

	if len(values) == 0 {
		return None[V]()
	}

	first := Some(values[0])

	*seq = func(yield func(V) bool) {
		for _, value := range values[1:] {
			if !yield(value) {
				return
			}
		}
	}

	return first
}

func intersection[V comparable](seq SeqSet[V], other Set[V]) SeqSet[V] {
	return func(yield func(V) bool) {
		seq(func(v V) bool {
			if other.Contains(v) {
				return yield(v)
			}
			return true
		})
	}
}
