package g

import (
	"context"
	"runtime"

	"github.com/enetx/iter"
)

// IterPar parallelizes the SeqMap using the specified number of workers.
func (seq SeqMap[K, V]) Parallel(workers ...Int) SeqMapPar[K, V] {
	numCPU := Int(runtime.NumCPU())
	count := Slice[Int](workers).Get(0).UnwrapOr(numCPU)

	if count.Lte(0) {
		count = numCPU
	}

	return SeqMapPar[K, V]{
		seq:     seq,
		workers: count,
		process: func(p Pair[K, V]) (Pair[K, V], bool) { return p, true },
	}
}

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
func (seq SeqMap[K, V]) Pull() (func() (K, V, bool), func()) { return iter.Pull2(iter.Seq2[K, V](seq)) }

// Take returns a new iterator with the first n elements.
// The function creates a new iterator containing the first n elements from the original iterator.
func (seq SeqMap[K, V]) Take(n uint) SeqMap[K, V] {
	return SeqMap[K, V](iter.Take2(iter.Seq2[K, V](seq), int(n)))
}

// Nth returns the nth key-value pair (0-indexed) in the sequence.
func (seq SeqMap[K, V]) Nth(n Int) Option[Pair[K, V]] {
	key, value, found := iter.Nth2(iter.Seq2[K, V](seq), int(n))
	if found {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// Keys returns an iterator containing all the keys in the ordered Map.
func (seq SeqMap[K, V]) Keys() SeqSlice[K] {
	return SeqSlice[K](iter.Keys(iter.Seq2[K, V](seq)))
}

// Values returns an iterator containing all the values in the ordered Map.
func (seq SeqMap[K, V]) Values() SeqSlice[V] {
	return SeqSlice[V](iter.Values(iter.Seq2[K, V](seq)))
}

// Chain creates a new iterator by concatenating the current iterator with other iterators.
//
// The function concatenates the key-value pairs from the current iterator with the key-value pairs from the provided iterators,
// producing a new iterator containing all concatenated elements.
//
// Params:
//
// - seqs ([]SeqMap[K, V]): Other iterators to be concatenated with the current iterator.
//
// Returns:
//
// - SeqMap[K, V]: A new iterator containing elements from the current iterator and the provided iterators.
//
// Example usage:
//
//	iter1 := g.NewMap[int, string]().Set(1, "a").Iter()
//	iter2 := g.NewMap[int, string]().Set(2, "b").Iter()
//
//	// Concatenating iterators and collecting the result.
//	iter1.Chain(iter2).Collect().Print()
//
// Output: Map{1:a, 2:b} // The output order may vary as Map is not ordered.
//
// The resulting iterator will contain elements from both iterators.
func (seq SeqMap[K, V]) Chain(seqs ...SeqMap[K, V]) SeqMap[K, V] {
	iterSeqs := make([]iter.Seq2[K, V], len(seqs))
	for i, s := range seqs {
		iterSeqs[i] = iter.Seq2[K, V](s)
	}

	return SeqMap[K, V](iter.Chain2(iter.Seq2[K, V](seq), iterSeqs...))
}

// Count consumes the iterator, counting the number of iterations and returning it.
func (seq SeqMap[K, V]) Count() Int { return Int(iter.Count2(iter.Seq2[K, V](seq))) }

// Collect collects all key-value pairs from the iterator and returns a Map.
func (seq SeqMap[K, V]) Collect() Map[K, V] {
	collection := NewMap[K, V]()

	seq(func(k K, v V) bool {
		collection[k] = v
		return true
	})

	return collection
}

// Filter returns a new iterator containing only the elements that satisfy the provided function.
//
// This function creates a new iterator containing key-value pairs for which the provided function returns true.
// It iterates through the current iterator, applying the function to each key-value pair.
// If the function returns true for a key-value pair, it will be included in the resulting iterator.
//
// Params:
//
// - fn (func(K, V) bool): The function applied to each key-value pair to determine inclusion.
//
// Returns:
//
// - SeqMap[K, V]: An iterator containing elements that satisfy the given function.
//
//	m := g.NewMap[int, int]().
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	even := m.Iter().
//		Filter(
//			func(k, v int) bool {
//				return v%2 == 0
//			}).
//		Collect()
//	even.Print()
//
// Output: Map{2:2, 4:4} // The output order may vary as Map is not ordered.
//
// The resulting iterator will contain elements for which the function returns true.
func (seq SeqMap[K, V]) Filter(fn func(K, V) bool) SeqMap[K, V] {
	return SeqMap[K, V](iter.Filter2(iter.Seq2[K, V](seq), fn))
}

// Exclude returns a new iterator excluding elements that satisfy the provided function.
//
// This function creates a new iterator excluding key-value pairs for which the provided function returns true.
// It iterates through the current iterator, applying the function to each key-value pair.
// If the function returns true for a key-value pair, it will be excluded from the resulting iterator.
//
// Params:
//
// - fn (func(K, V) bool): The function applied to each key-value pair to determine exclusion.
//
// Returns:
//
// - SeqMap[K, V]: An iterator excluding elements that satisfy the given function.
//
// Example usage:
//
//	m := g.NewMap[int, int]().
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	notEven := m.Iter().
//		Exclude(
//			func(k, v int) bool {
//				return v%2 == 0
//			}).
//		Collect()
//	notEven.Print()
//
// Output: Map{1:1, 3:3, 5:5} // The output order may vary as Map is not ordered.
//
// The resulting iterator will exclude elements for which the function returns true.
func (seq SeqMap[K, V]) Exclude(fn func(K, V) bool) SeqMap[K, V] {
	return SeqMap[K, V](iter.Exclude2(iter.Seq2[K, V](seq), fn))
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
//	m := g.NewMap[int, int]()
//	m.Set(1, 1)
//	f := m.Iter().Find(func(_ int, v int) bool { return v == 1 })
//	if f.IsSome() {
//		print(f.Some().Key)
//	}
//
// The resulting Option may contain the first element that satisfies the condition, or None if not found.
func (seq SeqMap[K, V]) Find(fn func(k K, v V) bool) Option[Pair[K, V]] {
	key, value, found := iter.Find2(iter.Seq2[K, V](seq), fn)
	if found {
		return Some(Pair[K, V]{Key: key, Value: value})
	}

	return None[Pair[K, V]]()
}

// ForEach iterates through all elements and applies the given function to each key-value pair.
//
// This function traverses the entire iterator and applies the provided function to each key-value pair.
// It iterates through the current iterator, executing the function on each key-value pair.
//
// Params:
//
// - fn (func(K, V)): The function to be applied to each key-value pair in the iterator.
//
// Example usage:
//
//	m := g.NewMap[int, int]().
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	mmap := m.Iter().
//		Map(
//			func(k, v int) (int, int) {
//				return k * k, v * v
//			}).
//		Collect()
//
//	mmap.Print()
//
// Output: Map{1:1, 4:4, 9:9, 16:16, 25:25} // The output order may vary as Map is not ordered.
//
// The function fn will be executed for each key-value pair in the iterator.
func (seq SeqMap[K, V]) ForEach(fn func(k K, v V)) { iter.ForEach2(iter.Seq2[K, V](seq), fn) }

// Inspect creates a new iterator that wraps around the current iterator
// and allows inspecting each key-value pair as it passes through.
func (seq SeqMap[K, V]) Inspect(fn func(k K, v V)) SeqMap[K, V] {
	return SeqMap[K, V](iter.Inspect2(iter.Seq2[K, V](seq), fn))
}

// Map creates a new iterator by applying the given function to each key-value pair.
//
// This function generates a new iterator by traversing the current iterator and applying the provided
// function to each key-value pair. It transforms the key-value pairs according to the given function.
//
// Params:
//
//   - fn (func(K, V) (K, V)): The function to be applied to each key-value pair in the iterator.
//     It takes a key-value pair and returns a new transformed key-value pair.
//
// Returns:
//
// - SeqMap[K, V]: A new iterator containing key-value pairs transformed by the provided function.
//
// Example usage:
//
//	m := g.NewMap[int, int]().
//		Set(1, 1).
//		Set(2, 2).
//		Set(3, 3).
//		Set(4, 4).
//		Set(5, 5)
//
//	mmap := m.Iter().
//		Map(
//			func(k, v int) (int, int) {
//				return k * k, v * v
//			}).
//		Collect()
//
//	mmap.Print()
//
// Output: Map{1:1, 4:4, 9:9, 16:16, 25:25} // The output order may vary as Map is not ordered.
//
// The resulting iterator will contain key-value pairs transformed by the given function.
func (seq SeqMap[K, V]) Map(transform func(K, V) (K, V)) SeqMap[K, V] {
	return SeqMap[K, V](iter.Map2(iter.Seq2[K, V](seq), transform))
}

// FilterMap applies a function to each key-value pair and filters out None results.
//
// The function transforms and filters pairs in a single pass. Pairs where the function
// returns None are filtered out, and pairs where it returns Some are unwrapped
// and included in the result.
//
// Params:
//
//   - fn (func(K, V) Option[Pair[K, V]]): The function that transforms and filters pairs.
//     Returns Some(Pair{key, value}) to include the transformed pair, or None to filter it out.
//
// Returns:
//
// - SeqMap[K, V]: A sequence containing only the successfully transformed pairs.
//
// Example usage:
//
//	configs := g.Map[string, string]{"host": "localhost", "port": "8080", "debug": "invalid"}
//	validConfigs := configs.Iter().FilterMap(func(k string, v string) Option[Pair[string, string]] {
//		if k == "port" || k == "host" {
//			return Some(Pair[string, string]{Key: k, Value: v + "_validated"})
//		}
//		return None[Pair[string, string]]()
//	})
//	// validConfigs will yield: {"host": "localhost_validated", "port": "8080_validated"}
//
//	users := g.Map[string, int]{"alice": 25, "bob": 17, "charlie": 30}
//	adults := users.Iter().FilterMap(func(name string, age int) Option[Pair[string, int]] {
//		if age >= 18 {
//			return Some(Pair[string, int]{Key: name, Value: age})
//		}
//		return None[Pair[string, int]]()
//	})
//	// adults will yield: {"alice": 25, "charlie": 30}
func (seq SeqMap[K, V]) FilterMap(fn func(K, V) Option[Pair[K, V]]) SeqMap[K, V] {
	return SeqMap[K, V](iter.FilterMap2(iter.Seq2[K, V](seq), func(k K, v V) (iter.Pair[K, V], bool) {
		return fn(k, v).Option()
	}))
}

// The iteration will stop when the provided function returns false for an element.
func (seq SeqMap[K, V]) Range(fn func(k K, v V) bool) { iter.Range2(iter.Seq2[K, V](seq), fn) }

// Context allows the iteration to be controlled with a context.Context.
func (seq SeqMap[K, V]) Context(ctx context.Context) SeqMap[K, V] {
	return SeqMap[K, V](iter.Context2(iter.Seq2[K, V](seq), ctx))
}

// Next extracts the next key-value pair from the iterator and advances it.
//
// This method consumes the next key-value pair from the iterator and returns them wrapped in an Option.
// The iterator itself is modified to point to the remaining elements.
//
// Returns:
// - Option[Pair[K, V]]: Some(Pair{Key, Value}) if a pair exists, None if the iterator is exhausted.
func (seq *SeqMap[K, V]) Next() Option[Pair[K, V]] {
	var pairs []Pair[K, V]

	(*seq)(func(k K, v V) bool {
		pairs = append(pairs, Pair[K, V]{Key: k, Value: v})
		return true
	})

	if len(pairs) == 0 {
		return None[Pair[K, V]]()
	}

	first := Some(pairs[0])

	*seq = func(yield func(K, V) bool) {
		for _, pair := range pairs[1:] {
			if !yield(pair.Key, pair.Value) {
				return
			}
		}
	}

	return first
}
