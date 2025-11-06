package g

import (
	"context"

	"github.com/enetx/g/cmp"
	"github.com/enetx/iter"
)

// Pull converts the “push-style” iterator sequence seq
// into a “pull-style” iterator accessed by the two functions
// next and stop.
//
// Next returns the next pair in the sequence
// and a boolean indicating whether the pair is valid.
// When the sequence is over, next returns a pair of zero values and false.
// It is valid to call next after reaching the end of the sequence
// or after calling stop. These calls will continue
// to return a pair of zero values and false.
//
// Stop ends the iteration. It must be called when the caller is
// no longer interested in next values and next has not yet
// signaled that the sequence is over (with a false boolean return).
// It is valid to call stop multiple times and when next has
// already returned false.
//
// It is an error to call next or stop from multiple goroutines
// simultaneously.
func (seq SeqMapOrd[K, V]) Pull() (func() (K, V, bool), func()) {
	return iter.Pull2(iter.Seq2[K, V](seq))
}

// Keys returns an iterator containing all the keys in the ordered Map.
func (seq SeqMapOrd[K, V]) Keys() SeqSlice[K] {
	return SeqSlice[K](iter.Keys(iter.Seq2[K, V](seq)))
}

// Values returns an iterator containing all the values in the ordered Map.
func (seq SeqMapOrd[K, V]) Values() SeqSlice[V] {
	return SeqSlice[V](iter.Values(iter.Seq2[K, V](seq)))
}

// Unzip returns a tuple of slices containing keys and values from the ordered map.
func (seq SeqMapOrd[K, V]) Unzip() (SeqSlice[K], SeqSlice[V]) { return seq.Keys(), seq.Values() }

// SortBy applies a custom sorting function to the elements in the iterator
// and returns a new iterator containing the sorted elements.
//
// The sorting function 'fn' should take two arguments, 'a' and 'b', of type Pair[K, V],
// and return true if 'a' should be ordered before 'b', and false otherwise.
//
// Example:
//
//	m := g.NewMapOrd[g.Int, g.String]()
//	m.
//		Set(6, "bb").
//		Set(0, "dd").
//		Set(1, "aa").
//		Set(5, "xx").
//		Set(2, "cc").
//		Set(3, "ff").
//		Set(4, "zz").
//		Iter().
//		SortBy(
//			func(a, b g.Pair[g.Int, g.String]) cmp.Ordering {
//				return a.Key.Cmp(b.Key)
//				// return a.Value.Cmp(b.Value)
//			}).
//		Collect().
//		Print()
//
// Output: MapOrd{0:dd, 1:aa, 2:cc, 3:ff, 4:zz, 5:xx, 6:bb}
//
// The returned iterator is of type SeqMapOrd[K, V], which implements the iterator
// interface for further iteration over the sorted elements.
func (seq SeqMapOrd[K, V]) SortBy(fn func(a, b Pair[K, V]) cmp.Ordering) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](
		iter.SortBy2(iter.Seq2[K, V](seq), func(a, b iter.Pair[K, V]) bool { return fn(a, b) == cmp.Less }),
	)
}

// SortByKey applies a custom sorting function to the keys in the iterator
// and returns a new iterator containing the sorted elements.
//
// The sorting function 'fn' should take two arguments, 'a' and 'b', of type K,
// and return true if 'a' should be ordered before 'b', and false otherwise.
//
// Example:
//
//	m := g.NewMapOrd[g.Int, g.String]()
//	m.
//		Set(6, "bb").
//		Set(0, "dd").
//		Set(1, "aa").
//		Set(5, "xx").
//		Set(2, "cc").
//		Set(3, "ff").
//		Set(4, "zz").
//		Iter().
//		SortByKey(g.Int.Cmp).
//		Collect().
//		Print()
//
// Output: MapOrd{0:dd, 1:aa, 2:cc, 3:ff, 4:zz, 5:xx, 6:bb}
func (seq SeqMapOrd[K, V]) SortByKey(fn func(a, b K) cmp.Ordering) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.OrderByKey(iter.Seq2[K, V](seq), func(a, b K) bool { return fn(a, b) == cmp.Less }))
}

// SortByValue applies a custom sorting function to the values in the iterator
// and returns a new iterator containing the sorted elements.
//
// The sorting function 'fn' should take two arguments, 'a' and 'b', of type V,
// and return true if 'a' should be ordered before 'b', and false otherwise.
//
// Example:
//
//	m := g.NewMapOrd[g.Int, g.String]()
//	m.
//		Set(6, "bb").
//		Set(0, "dd").
//		Set(1, "aa").
//		Set(5, "xx").
//		Set(2, "cc").
//		Set(3, "ff").
//		Set(4, "zz").
//		Iter().
//		SortByValue(g.String.Cmp).
//		Collect().
//		Print()
//
// Output: MapOrd{1:aa, 6:bb, 2:cc, 0:dd, 3:ff, 5:xx, 4:zz}
func (seq SeqMapOrd[K, V]) SortByValue(fn func(a, b V) cmp.Ordering) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.OrderByValue(iter.Seq2[K, V](seq), func(a, b V) bool { return fn(a, b) == cmp.Less }))
}

// Inspect creates a new iterator that wraps around the current iterator
// and allows inspecting each key-value pair as it passes through.
func (seq SeqMapOrd[K, V]) Inspect(fn func(k K, v V)) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Inspect2(iter.Seq2[K, V](seq), fn))
}

// StepBy creates a new iterator that iterates over every N-th element of the original iterator.
// This function is useful when you want to skip a specific number of elements between each iteration.
//
// Parameters:
// - n int: The step size, indicating how many elements to skip between each iteration.
//
// Returns:
// - SeqMapOrd[K, V]: A new iterator that produces key-value pairs from the original iterator with a step size of N.
//
// Example usage:
//
//	mapIter := g.MapOrd[string, int]{{"one", 1}, {"two", 2}, {"three", 3}}.Iter()
//	iter := mapIter.StepBy(2)
//	result := iter.Collect()
//	result.Print()
//
// Output: MapOrd{one:1, three:3}
//
// The resulting iterator will produce key-value pairs from the original iterator with a step size of N.
func (seq SeqMapOrd[K, V]) StepBy(n uint) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.StepBy2(iter.Seq2[K, V](seq), int(n)))
}

// Chain concatenates the current iterator with other iterators, returning a new iterator.
//
// The function creates a new iterator that combines the elements of the current iterator
// with elements from the provided iterators in the order they are given.
//
// Params:
//
// - seqs ([]seqMapOrd[K, V]): Other iterators to be concatenated with the current iterator.
//
// Returns:
//
// - SeqMapOrd[K, V]: A new iterator containing elements from the current iterator and the provided iterators.
//
// Example usage:
//
//	iter1 := g.NewMapOrd[int, string]()
//	iter1.Set(1, "a").Iter()
//
//	iter2 := g.NewMapOrd[int, string]()
//	iter2.Set(2, "b").Iter()
//
//	// Concatenating iterators and collecting the result.
//	iter1.Chain(iter2).Collect().Print()
//
// Output: MapOrd{1:a, 2:b}
//
// The resulting iterator will contain elements from both iterators in the specified order.
func (seq SeqMapOrd[K, V]) Chain(seqs ...SeqMapOrd[K, V]) SeqMapOrd[K, V] {
	iterSeqs := make([]iter.Seq2[K, V], len(seqs))
	for i, s := range seqs {
		iterSeqs[i] = iter.Seq2[K, V](s)
	}

	return SeqMapOrd[K, V](iter.Chain2(iter.Seq2[K, V](seq), iterSeqs...))
}

// Count consumes the iterator, counting the number of iterations and returning it.
func (seq SeqMapOrd[K, V]) Count() Int { return Int(iter.Count2(iter.Seq2[K, V](seq))) }

// Collect collects all key-value pairs from the iterator and returns a MapOrd.
func (seq SeqMapOrd[K, V]) Collect() MapOrd[K, V] {
	collection := NewMapOrd[K, V]()

	seq(func(k K, v V) bool {
		collection.Set(k, v)
		return true
	})

	return collection
}

// Skip returns a new iterator skipping the first n elements.
//
// The function creates a new iterator that skips the first n elements of the current iterator
// and returns an iterator starting from the (n+1)th element.
//
// Params:
//
// - n (uint): The number of elements to skip from the beginning of the iterator.
//
// Returns:
//
// - SeqMapOrd[K, V]: An iterator that starts after skipping the first n elements.
//
// Example usage:
//

//	iter := g.NewMapOrd[int, string]()
//	iter.
//		Set(1, "a").
//		Set(2, "b").
//		Set(3, "c").
//		Set(4, "d").
//		Iter()
//
//	// Skipping the first two elements and collecting the rest.
//	iter.Skip(2).Collect().Print()
//
// Output: MapOrd{3:c, 4:d}
//
// The resulting iterator will start after skipping the specified number of elements.
func (seq SeqMapOrd[K, V]) Skip(n uint) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Skip2(iter.Seq2[K, V](seq), int(n)))
}

// Exclude returns a new iterator excluding elements that satisfy the provided function.
//
// The function creates a new iterator excluding elements from the current iterator
// for which the provided function returns true.
//
// Params:
//
// - fn (func(K, V) bool): The function used to determine exclusion criteria for elements.
//
// Returns:
//
// - SeqMapOrd[K, V]: A new iterator excluding elements that satisfy the given condition.
//
// Example usage:
//
//	mo := g.NewMapOrd[int, int]()
//	mo.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	notEven := mo.Iter().
//		Exclude(
//			func(k, v int) bool {
//				return v%2 == 0
//			}).
//		Collect()
//	notEven.Print()
//
// Output: MapOrd{1:1, 3:3, 5:5}
//
// The resulting iterator will exclude elements based on the provided condition.
func (seq SeqMapOrd[K, V]) Exclude(fn func(K, V) bool) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Exclude2(iter.Seq2[K, V](seq), fn))
}

// Filter returns a new iterator containing only the elements that satisfy the provided function.
//
// The function creates a new iterator including elements from the current iterator
// for which the provided function returns true.
//
// Params:
//
// - fn (func(K, V) bool): The function used to determine inclusion criteria for elements.
//
// Returns:
//
// - SeqMapOrd[K, V]: A new iterator containing elements that satisfy the given condition.
//
// Example usage:
//
//	mo := g.NewMapOrd[int, int]()
//	mo.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	even := mo.Iter().
//		Filter(
//			func(k, v int) bool {
//				return v%2 == 0
//			}).
//		Collect()
//	even.Print()
//
// Output: MapOrd{2:2, 4:4}
//
// The resulting iterator will include elements based on the provided condition.
func (seq SeqMapOrd[K, V]) Filter(fn func(K, V) bool) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Filter2(iter.Seq2[K, V](seq), fn))
}

// Find searches for an element in the iterator that satisfies the provided function.
//
// The function iterates through the elements of the iterator and returns the first element
// for which the provided function returns true.
//
// Params:
//
// - fn (func(K, V) bool): The function used to test elements for a condition.
//
// Returns:
//
// - Option[K, V]: An Option containing the first element that satisfies the condition; None if not found.
//
// Example usage:
//
//	m := g.NewMapOrd[int, int]()
//	m.Set(1, 1)
//	f := m.Iter().Find(func(_ int, v int) bool { return v == 1 })
//	if f.IsSome() {
//		print(f.Some().Key)
//	}
//
// The resulting Option may contain the first element that satisfies the condition, or None if not found.
func (seq SeqMapOrd[K, V]) Find(fn func(k K, v V) bool) Option[Pair[K, V]] {
	key, value, found := iter.Find2(iter.Seq2[K, V](seq), fn)
	if found {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// ForEach iterates through all elements and applies the given function to each key-value pair.
//
// The function applies the provided function to each key-value pair in the iterator.
//
// Params:
//
// - fn (func(K, V)): The function to be applied to each key-value pair in the iterator.
//
// Example usage:
//
//	iter := g.NewMapOrd[int, int]()
//	iter.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5).
//		Iter()
//
//	iter.ForEach(func(key K, val V) {
//	    // Process key-value pair
//	})
//
// The provided function will be applied to each key-value pair in the iterator.
func (seq SeqMapOrd[K, V]) ForEach(fn func(k K, v V)) {
	iter.ForEach2(iter.Seq2[K, V](seq), fn)
}

// Map creates a new iterator by applying the given function to each key-value pair.
//
// The function creates a new iterator by applying the provided function to each key-value pair in the iterator.
//
// Params:
//
// - fn (func(K, V) (K, V)): The function used to transform each key-value pair in the iterator.
//
// Returns:
//
// - SeqMapOrd[K, V]: A new iterator containing transformed key-value pairs.
//
// Example usage:
//
//	mo := g.NewMapOrd[int, int]()
//	mo.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	momap := mo.Iter().
//		Map(
//			func(k, v int) (int, int) {
//				return k * k, v * v
//			}).
//		Collect()
//
//	momap.Print()
//
// Output: MapOrd{1:1, 4:4, 9:9, 16:16, 25:25}
//
// The resulting iterator will contain transformed key-value pairs.
func (seq SeqMapOrd[K, V]) Map(transform func(K, V) (K, V)) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Map2(iter.Seq2[K, V](seq), transform))
}

// Range iterates through elements until the given function returns false.
//
// The function iterates through the key-value pairs in the iterator, applying the provided function to each pair.
// It continues iterating until the function returns false.
//
// Params:
//
// - fn (func(K, V) bool): The function to be applied to each key-value pair in the iterator.
//
// Example usage:
//
//	iter := g.NewMapOrd[int, int]()
//	iter.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5).
//		Iter()
//
//	iter.Range(func(k, v int) bool {
//	    fmt.Println(v) // Replace this with the function logic you need.
//	    return v < 5 // Replace this with the condition for continuing iteration.
//	})
//
// The iteration will stop when the provided function returns false.
func (seq SeqMapOrd[K, V]) Range(fn func(k K, v V) bool) {
	iter.Range2(iter.Seq2[K, V](seq), fn)
}

// Context allows the iteration to be controlled with a context.Context.
func (seq SeqMapOrd[K, V]) Context(ctx context.Context) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Context2(iter.Seq2[K, V](seq), ctx))
}

// Take returns a new iterator with the first n elements.
// The function creates a new iterator containing the first n elements from the original iterator.
func (seq SeqMapOrd[K, V]) Take(n uint) SeqMapOrd[K, V] {
	return SeqMapOrd[K, V](iter.Take2(iter.Seq2[K, V](seq), int(n)))
}

// First returns the first key-value pair from the sequence.
func (seq SeqMapOrd[K, V]) First() Option[Pair[K, V]] {
	if key, value, ok := iter.First2(iter.Seq2[K, V](seq)); ok {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// Last returns the last key-value pair from the sequence.
func (seq SeqMapOrd[K, V]) Last() Option[Pair[K, V]] {
	if key, value, ok := iter.Last2(iter.Seq2[K, V](seq)); ok {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// Nth returns the nth key-value pair (0-indexed) in the sequence.
func (seq SeqMapOrd[K, V]) Nth(n Int) Option[Pair[K, V]] {
	key, value, found := iter.Nth2(iter.Seq2[K, V](seq), int(n))
	if found {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// ToChan converts the iterator into a channel, optionally with context(s).
//
// The function converts the key-value pairs from the iterator into a channel, allowing iterative processing
// using channels. It can be used to stream key-value pairs for concurrent or asynchronous operations.
//
// Params:
//
// - ctxs (...context.Context): Optional context(s) that can be used to cancel or set deadlines for the operation.
//
// Returns:
//
// - chan Pair[K, V]: A channel emitting key-value pairs from the iterator.
//
// Example usage:
//
//	iter := g.NewMapOrd[int, int]()
//	iter.
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5).
//		Iter()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel() // Ensure cancellation to avoid goroutine leaks.
//
//	ch := iter.ToChan(ctx)
//	for pair := range ch {
//	    // Process key-value pair from the channel
//	}
//
// The function converts the iterator into a channel to allow sequential or concurrent processing of key-value pairs.
func (seq SeqMapOrd[K, V]) ToChan(ctxs ...context.Context) chan Pair[K, V] {
	ctx := context.Background()
	if len(ctxs) > 0 {
		ctx = ctxs[0]
	}

	return iter.ToChan2(iter.Seq2[K, V](seq), ctx)
}

// Next extracts the next key-value pair from the iterator and advances it.
//
// This method consumes the next key-value pair from the iterator and returns them wrapped in an Option.
// The iterator itself is modified to point to the remaining elements.
//
// Returns:
// - Option[Pair[K, V]]: Some(Pair{Key, Value}) if a pair exists, None if the iterator is exhausted.
func (seq *SeqMapOrd[K, V]) Next() Option[Pair[K, V]] {
	if key, value, remaining, ok := iter.Next2(iter.Seq2[K, V](*seq)); ok {
		*seq = SeqMapOrd[K, V](remaining)
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}
