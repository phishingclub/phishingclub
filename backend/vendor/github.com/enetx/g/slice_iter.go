package g

import (
	"context"
	"reflect"
	"runtime"

	"github.com/enetx/g/cmp"
	"github.com/enetx/g/constraints"
	"github.com/enetx/g/f"
	"github.com/enetx/iter"
)

// Parallel runs this SeqSlice in parallel using the given number of workers.
func (seq SeqSlice[V]) Parallel(workers ...Int) SeqSlicePar[V] {
	numCPU := Int(runtime.NumCPU())
	count := Slice[Int](workers).Get(0).UnwrapOr(numCPU)

	if count.Lte(0) {
		count = numCPU
	}

	return SeqSlicePar[V]{
		seq:     seq,
		workers: count,
		process: func(v V) (V, bool) { return v, true },
	}
}

// Range returns a SeqSlice[T] yielding a sequence of integers of type T,
// starting at start, incrementing by step, and ending before stop (exclusive).
//
//   - If step is omitted, it defaults to 1.
//   - If step is 0, the sequence is empty.
//   - If step does not move toward stop (e.g., positive step with start > stop),
//     the sequence is empty.
//
// Examples:
//   - Range(0, 5) yields [0, 1, 2, 3, 4]
//   - Range(5, 0, -1) yields [5, 4, 3, 2, 1]
func Range[T constraints.Integer](start, stop T, step ...T) SeqSlice[T] {
	return SeqSlice[T](iter.Iota(start, stop, step...))
}

// RangeInclusive returns a SeqSlice[T] yielding a sequence of integers of type T,
// starting at start, incrementing by step, and ending at stop (inclusive).
//
//   - If step is omitted, it defaults to 1.
//   - If step is 0, the sequence is empty.
//   - If step does not move toward stop (e.g., positive step with start > stop),
//     the sequence is empty.
//
// Examples:
//   - RangeInclusive(0, 5) yields [0, 1, 2, 3, 4, 5]
//   - RangeInclusive(5, 0, -1) yields [5, 4, 3, 2, 1, 0]
func RangeInclusive[T constraints.Integer](start, stop T, step ...T) SeqSlice[T] {
	return SeqSlice[T](iter.IotaInclusive(start, stop, step...))
}

// Pull converts the "push-style" iterator sequence seq
// into a "pull-style" iterator accessed by the two functions
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
func (seq SeqSlice[V]) Pull() (func() (V, bool), func()) { return iter.Pull(iter.Seq[V](seq)) }

// All checks whether all elements in the iterator satisfy the provided condition.
// This function is useful when you want to determine if all elements in an iterator
// meet a specific criteria.
//
// Parameters:
// - fn func(V) bool: A function that returns a boolean indicating whether the element satisfies
// the condition.
//
// Returns:
// - bool: True if all elements in the iterator satisfy the condition, false otherwise.
//
// Example usage:
//
//	slice := g.SliceOf(1, 2, 3, 4, 5, 6, 7, -1, -2)
//	isPositive := func(num int) bool { return num > 0 }
//	allPositive := slice.Iter().All(isPositive)
//
// The resulting allPositive will be true if all elements returned by the iterator are positive.
func (seq SeqSlice[V]) All(fn func(v V) bool) bool { return iter.All(iter.Seq[V](seq), fn) }

// Any checks whether any element in the iterator satisfies the provided condition.
// This function is useful when you want to determine if at least one element in an iterator
// meets a specific criteria.
//
// Parameters:
// - fn func(V) bool: A function that returns a boolean indicating whether the element satisfies
// the condition.
//
// Returns:
// - bool: True if at least one element in the iterator satisfies the condition, false otherwise.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 3, 5, 7, 9}
//	isEven := func(num int) bool { return num%2 == 0 }
//	anyEven := slice.Iter().Any(isEven)
//
// The resulting anyEven will be true if at least one element returned by the iterator is even.
func (seq SeqSlice[V]) Any(fn func(V) bool) bool { return iter.Any(iter.Seq[V](seq), fn) }

// Chain concatenates the current iterator with other iterators, returning a new iterator.
//
// The function creates a new iterator that combines the elements of the current iterator
// with elements from the provided iterators in the order they are given.
//
// Params:
//
// - seqs ([]SeqSlice[V]): Other iterators to be concatenated with the current iterator.
//
// Returns:
//
// - sequence[V]: A new iterator containing elements from the current iterator and the provided iterators.
//
// Example usage:
//
//	iter1 := g.Slice[int]{1, 2, 3}.Iter()
//	iter2 := g.Slice[int]{4, 5, 6}.Iter()
//	iter1.Chain(iter2).Collect().Print()
//
// Output: [1, 2, 3, 4, 5, 6]
//
// The resulting iterator will contain elements from both iterators in the specified order.
func (seq SeqSlice[V]) Chain(seqs ...SeqSlice[V]) SeqSlice[V] {
	iterSeqs := make([]iter.Seq[V], len(seqs))
	for i, s := range seqs {
		iterSeqs[i] = iter.Seq[V](s)
	}

	return SeqSlice[V](iter.Chain(iter.Seq[V](seq), iterSeqs...))
}

// Chunks returns an iterator that yields chunks of elements of the specified size.
//
// The function creates a new iterator that yields chunks of elements from the original iterator,
// with each chunk containing elements of the specified size.
//
// Params:
//
// - n (Int): The size of each chunk.
//
// Returns:
//
// - SeqSlices[V]: An iterator yielding chunks of elements of the specified size.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5, 6}
//	chunks := slice.Iter().Chunks(2).Collect()
//
// Output: [Slice[1, 2] Slice[3, 4] Slice[5, 6]]
//
// The resulting iterator will yield chunks of elements, each containing the specified number of elements.
func (seq SeqSlice[V]) Chunks(n Int) SeqSlices[V] {
	return SeqSlices[V](iter.Chunks(iter.Seq[V](seq), int(n)))
}

// Collect gathers all elements from the iterator into a Slice.
func (seq SeqSlice[V]) Collect() Slice[V] { return iter.ToSlice(iter.Seq[V](seq)) }

// Collect gathers all elements from the iterator into a []Slice.
func (seqs SeqSlices[V]) Collect() []Slice[V] {
	collection := make([]Slice[V], 0)

	seqs(func(v []V) bool {
		inner := SeqSlice[V](iter.FromSlice(v)).Collect()
		collection = append(collection, inner)
		return true
	})

	return collection
}

// Count consumes the iterator, counting the number of iterations and returning it.
func (seq SeqSlice[V]) Count() Int { return Int(iter.Count(iter.Seq[V](seq))) }

// Counter returns a map where each key is a unique element
// from the slice and each value is the count of how many times that element appears.
//
// The function counts the occurrences of each element in the slice
// and returns a map representing the unique elements and their respective counts.
// This method uses iter.Counter from the iter package.
//
// Returns:
//
// - map[any]Int: with keys representing the unique elements in the slice
// and values representing the counts of those elements.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 1, 2, 1}
//	counts := slice.Iter().Counter()
//	// The counts map will contain:
//	// 1 -> 3 (since 1 appears three times)
//	// 2 -> 2 (since 2 appears two times)
//	// 3 -> 1 (since 3 appears once)
func (seq SeqSlice[V]) Counter() SeqMapOrd[any, Int] {
	return func(yield func(any, Int) bool) {
		for k, v := range iter.Counter(iter.Seq[V](seq)) {
			if !yield(k, Int(v)) {
				return
			}
		}
	}
}

// GroupBy groups consecutive elements of the sequence based on a custom equality function.
//
// The provided function `fn` takes two consecutive elements `a` and `b` and returns `true`
// if they belong to the same group, or `false` if a new group should start.
// The function returns a `SeqSlices[V]`, where each `[]V` represents a group of consecutive
// elements that satisfy the provided equality condition.
//
// Notes:
//   - Each group is returned as a copy of the elements, since `SeqSlice` does not guarantee
//     that elements share the same backing array.
//
// Parameters:
//   - fn (func(a, b V) bool): Function that determines whether two consecutive elements belong to the same group.
//
// Returns:
//   - SeqSlices[V]: An iterator yielding slices, each containing one group.
//
// Example usage:
//
//	slice := g.SliceOf(1, 1, 2, 3, 2, 3, 4)
//	groups := slice.Iter().GroupBy(func(a, b int) bool { return a <= b }).Collect()
//	// Output: [Slice[1, 1, 2, 3] Slice[2, 3, 4]]
//
// The resulting iterator will yield groups of consecutive elements according to the provided function.
func (seq SeqSlice[V]) GroupBy(fn func(a, b V) bool) SeqSlices[V] {
	return SeqSlices[V](iter.GroupByAdjacent(iter.Seq[V](seq), fn))
}

// Combinations generates all combinations of length 'n' from the sequence.
func (seq SeqSlice[V]) Combinations(size Int) SeqSlices[V] {
	return SeqSlices[V](iter.Combinations(iter.Seq[V](seq), int(size)))
}

// Cycle returns an iterator that endlessly repeats the elements of the current sequence.
func (seq SeqSlice[V]) Cycle() SeqSlice[V] {
	return SeqSlice[V](iter.Cycle(iter.Seq[V](seq)))
}

// Enumerate adds an index to each element in the iterator.
//
// Returns:
//
// - SeqMapOrd[Int, V] An iterator with each element of type Pair[Int, V], where the first
// element of the pair is the index and the second element is the original element from the
// iterator.
//
// Example usage:
//
//	ps := g.SliceOf[g.String]("bbb", "ddd", "xxx", "aaa", "ccc").
//		Iter().
//		Enumerate().
//		Collect()
//
//	ps.Print()
//
// Output: MapOrd{0:bbb, 1:ddd, 2:xxx, 3:aaa, 4:ccc}
func (seq SeqSlice[V]) Enumerate() SeqMapOrd[Int, V] {
	return func(yield func(Int, V) bool) {
		iterEnum := iter.Enumerate(iter.Seq[V](seq), 0)
		iterEnum(func(i int, v V) bool {
			return yield(Int(i), v)
		})
	}
}

// Dedup creates a new iterator that removes consecutive duplicate elements from the original iterator,
// leaving only one occurrence of each unique element. If the iterator is sorted, all elements will be unique.
//
// Parameters:
// - None
//
// Returns:
// - SeqSlice[V]: A new iterator with consecutive duplicates removed.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 2, 3, 4, 4, 4, 5}
//	iter := slice.Iter().Dedup()
//	result := iter.Collect()
//	result.Print()
//
// Output: [1 2 3 4 5]
//
// The resulting iterator will contain only unique elements, removing consecutive duplicates.
func (seq SeqSlice[V]) Dedup() SeqSlice[V] {
	return SeqSlice[V](iter.DedupBy(iter.Seq[V](seq), func(a, b V) bool {
		if f.IsComparable(a) {
			return f.Eq[any](a)(b)
		}
		return f.Eqd(a)(b)
	}))
}

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
// - SeqSlice[V]: A new iterator containing the elements that satisfy the given condition.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	even := slice.Iter().
//		Filter(
//			func(val int) bool {
//				return val%2 == 0
//			}).
//		Collect()
//	even.Print()
//
// Output: [2 4].
//
// The resulting iterator will contain only the elements that satisfy the provided function.
func (seq SeqSlice[V]) Filter(fn func(V) bool) SeqSlice[V] {
	return SeqSlice[V](iter.Filter(iter.Seq[V](seq), fn))
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
// - SeqSlice[V]: A new iterator containing the elements that do not satisfy the given condition.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	notEven := slice.Iter().
//		Exclude(
//			func(val int) bool {
//				return val%2 == 0
//			}).
//		Collect()
//	notEven.Print()
//
// Output: [1, 3, 5]
//
// The resulting iterator will contain only the elements that do not satisfy the provided function.
func (seq SeqSlice[V]) Exclude(fn func(V) bool) SeqSlice[V] {
	return SeqSlice[V](iter.Exclude(iter.Seq[V](seq), fn))
}

// Fold accumulates values in the iterator using a function.
//
// The function iterates through the elements of the iterator, accumulating values
// using the provided function and an initial value.
//
// Params:
//
//   - init (V): The initial value for accumulation.
//   - fn (func(V, V) V): The function that accumulates values; it takes two arguments
//     of type V and returns a value of type V.
//
// Returns:
//
// - T: The accumulated value after applying the function to all elements.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	sum := slice.Iter().
//		Fold(0,
//			func(acc, val int) int {
//				return acc + val
//			})
//	fmt.Println(sum)
//
// Output: 15.
//
// The resulting value will be the accumulation of elements based on the provided function.
func (seq SeqSlice[V]) Fold(init V, fn func(acc, val V) V) V {
	return iter.Fold(iter.Seq[V](seq), init, fn)
}

// Reduce aggregates elements of the sequence using the provided function.
// The first element of the sequence is used as the initial accumulator value.
// If the sequence is empty, it returns None[V].
//
// Params:
//   - fn (func(V, V) V): Function that combines two values into one.
//
// Returns:
//   - Option[V]: The accumulated value wrapped in Some, or None if the sequence is empty.
//
// Example:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	product := slice.Iter().Reduce(func(a, b int) int { return a * b })
//	if product.IsSome() {
//	    fmt.Println(product.Some()) // 120
//	} else {
//	    fmt.Println("empty")
//	}
func (seq SeqSlice[V]) Reduce(fn func(a, b V) V) Option[V] {
	return OptionOf(iter.Reduce(iter.Seq[V](seq), fn))
}

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
//	iter := g.Slice[int]{1, 2, 3, 4, 5}.Iter()
//	iter.ForEach(func(val V) {
//	    fmt.Println(val) // Replace this with the function logic you need.
//	})
//
// The provided function will be applied to each element in the iterator.
func (seq SeqSlice[V]) ForEach(fn func(v V)) { iter.ForEach(iter.Seq[V](seq), fn) }

// Flatten flattens an iterator of iterators into a single iterator.
//
// The function creates a new iterator that flattens a sequence of iterators,
// returning a single iterator containing elements from each iterator in sequence.
//
// Returns:
//
// - SeqSlice[V]: A single iterator containing elements from the sequence of iterators.
//
// Example usage:
//
//	nestedSlice := g.Slice[any]{
//		1,
//		g.SliceOf(2, 3),
//		"abc",
//		g.SliceOf("def", "ghi"),
//		g.SliceOf(4.5, 6.7),
//	}
//
//	nestedSlice.Iter().Flatten().Collect().Print()
//
// Output: Slice[1, 2, 3, abc, def, ghi, 4.5, 6.7]
//
// The resulting iterator will contain elements from each iterator in sequence.
func (seq SeqSlice[V]) Flatten() SeqSlice[V] {
	return func(yield func(V) bool) {
		var flatten func(item any) bool
		flatten = func(item any) bool {
			rv := reflect.ValueOf(item)
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				for i := range rv.Len() {
					if !flatten(rv.Index(i).Interface()) {
						return false
					}
				}
			default:
				if v, ok := item.(V); ok {
					if !yield(v) {
						return false
					}
				}
			}
			return true
		}

		seq(func(item V) bool {
			return flatten(item)
		})
	}
}

// Inspect creates a new iterator that wraps around the current iterator
// and allows inspecting each element as it passes through.
func (seq SeqSlice[V]) Inspect(fn func(v V)) SeqSlice[V] {
	return SeqSlice[V](iter.Inspect(iter.Seq[V](seq), fn))
}

// Intersperse inserts the provided separator between elements of the iterator.
//
// The function creates a new iterator that inserts the given separator between each
// consecutive pair of elements in the original iterator.
//
// Params:
//
// - sep (V): The separator to intersperse between elements.
//
// Returns:
//
// - SeqSlice[V]: An iterator containing elements with the separator interspersed.
//
// Example usage:
//
//	g.Slice[string]{"Hello", "World", "!"}.
//		Iter().
//		Intersperse(" ").
//		Collect().
//		Join().
//		Print()
//
// Output: "Hello World !".
//
// The resulting iterator will contain elements with the separator interspersed.
func (seq SeqSlice[V]) Intersperse(sep V) SeqSlice[V] {
	return SeqSlice[V](iter.Intersperse(iter.Seq[V](seq), sep))
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
// - SeqSlice[V]: A iterator containing elements transformed by the provided function.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3}
//	doubled := slice.
//		Iter().
//		Map(
//			func(val int) int {
//				return val * 2
//			}).
//		Collect()
//	doubled.Print()
//
// Output: [2 4 6].
//
// The resulting iterator will contain elements transformed by the provided function.
func (seq SeqSlice[V]) Map(transform func(V) V) SeqSlice[V] {
	return SeqSlice[V](iter.Map(iter.Seq[V](seq), transform))
}

// FlatMap applies a function to each element that returns an iterator, then flattens the results.
//
// The function transforms each element into a sequence and then concatenates all sequences
// into a single flat sequence.
//
// Params:
//
// - fn (func(V) SeqSlice[V]): The function that transforms each element into a sequence.
//
// Returns:
//
// - SeqSlice[V]: A flattened sequence containing all elements from the transformed sequences.
//
// Example usage:
//
//	words := g.Slice[string]{"hello world", "foo bar"}.Iter()
//	chars := words.FlatMap(func(s string) SeqSlice[string] {
//		return g.String(s).Split("")
//	})
//	// chars will yield: "h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d", "f", "o", "o", " ", "b", "a", "r"
//
//	numbers := g.Slice[int]{1, 2, 3}.Iter()
//	expanded := numbers.FlatMap(func(n int) SeqSlice[int] {
//		return g.Slice[int]{n, n*10, n*100}.Iter()
//	})
//	// expanded will yield: 1, 10, 100, 2, 20, 200, 3, 30, 300
func (seq SeqSlice[V]) FlatMap(fn func(V) SeqSlice[V]) SeqSlice[V] {
	mapped := iter.MapTo(iter.Seq[V](seq), func(v V) iter.Seq[V] {
		return iter.Seq[V](fn(v))
	})

	return SeqSlice[V](iter.FlattenSeq(mapped))
}

// FilterMap applies a function to each element and filters out None results.
//
// The function transforms and filters in a single pass. Elements where the function
// returns None are filtered out, and elements where it returns Some are unwrapped
// and included in the result.
//
// Params:
//
//   - fn (func(V) Option[V]): The function that transforms and filters elements.
//     Returns Some(value) to include the transformed value, or None to filter it out.
//
// Returns:
//
// - SeqSlice[V]: A sequence containing only the successfully transformed elements.
//
// Example usage:
//
//	strings := g.Slice[string]{"1", "2", "abc", "3", "xyz"}.Iter()
//	numbers := strings.FilterMap(func(s string) Option[int] {
//		if n, err := strconv.Atoi(s); err == nil {
//			return Some(n)
//		}
//		return None[int]()
//	})
//	// numbers will yield: 1, 2, 3
//
//	values := g.Slice[int]{1, -2, 3, -4, 5}.Iter()
//	positiveDoubled := values.FilterMap(func(n int) Option[int] {
//		if n > 0 {
//			return Some(n * 2)
//		}
//		return None[int]()
//	})
//	// positiveDoubled will yield: 2, 6, 10
func (seq SeqSlice[V]) FilterMap(fn func(V) Option[V]) SeqSlice[V] {
	return SeqSlice[V](iter.FilterMap(iter.Seq[V](seq), func(v V) (V, bool) {
		return fn(v).Option()
	}))
}

// transformSeq converts a standard library iter.Seq[V] into a SeqSlice[U] by mapping each element
// with the provided transformation function `fn`.
func transformSeq[V, U any](seq func(func(V) bool), fn func(V) U) SeqSlice[U] {
	return func(yield func(U) bool) {
		seq(func(v V) bool {
			return yield(fn(v))
		})
	}
}

// Partition divides the elements of the iterator into two separate slices based on a given predicate function.
//
// The function takes a predicate function 'fn', which should return true or false for each element in the iterator.
// Elements for which 'fn' returns true are collected into the left slice, while those for which 'fn' returns false
// are collected into the right slice.
//
// Params:
//
// - fn (func(V) bool): The predicate function used to determine the placement of elements.
//
// Returns:
//
// - (Slice[V], Slice[V]): Two slices representing elements that satisfy and don't satisfy the predicate, respectively.
//
// Example usage:
//
//	evens, odds := g.Slice[int]{1, 2, 3, 4, 5}.
//		Iter().
//		Partition(
//			func(v int) bool {
//				return v%2 == 0
//			})
//
//	fmt.Println("Even numbers:", evens) // Output: Even numbers: Slice[2, 4]
//	fmt.Println("Odd numbers:", odds)   // Output: Odd numbers: Slice[1, 3, 5]
//
// The resulting two slices will contain elements separated based on whether they satisfy the predicate or not.
func (seq SeqSlice[V]) Partition(fn func(v V) bool) (Slice[V], Slice[V]) {
	return iter.Partition(iter.Seq[V](seq), fn)
}

// Permutations generates iterators of all permutations of elements.
//
// The function uses a recursive approach to generate all the permutations of the elements.
// If the iterator is empty or contains a single element, it returns the iterator itself
// wrapped in a single-element iterator.
//
// Returns:
//
// - SeqSlices[V]: An iterator of iterators containing all possible permutations of the
// elements in the iterator.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3}
//	perms := slice.Iter().Permutations().Collect()
//	for _, perm := range perms {
//	    fmt.Println(perm)
//	}
//
// Output:

// Slice[1, 2, 3]
// Slice[2, 1, 3]
// Slice[3, 1, 2]
// Slice[1, 3, 2]
// Slice[2, 3, 1]
// Slice[3, 2, 1]
//
// The resulting iterator will contain iterators representing all possible permutations
// of the elements in the original iterator.
func (seq SeqSlice[V]) Permutations() SeqSlices[V] {
	return SeqSlices[V](iter.Permutations(iter.Seq[V](seq)))
}

// Range iterates through elements until the given function returns false.
//
// The function iterates through the elements of the iterator and applies the provided function
// to each element. It stops iteration when the function returns false for an element.
//
// Params:
//
// - fn (func(V) bool): The function that evaluates elements for continuation of iteration.
//
// Example usage:
//
//	iter := g.Slice[int]{1, 2, 3, 4, 5}.Iter()
//	iter.Range(func(val int) bool {
//	    fmt.Println(val) // Replace this with the function logic you need.
//	    return val < 5 // Replace this with the condition for continuing iteration.
//	})
//
// The iteration will stop when the provided function returns false for an element.
func (seq SeqSlice[V]) Range(fn func(v V) bool) { iter.Range(iter.Seq[V](seq), fn) }

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
// - SeqSlice[V]: An iterator that starts after skipping the first n elements.
//
// Example usage:
//
//	iter := g.Slice[int]{1, 2, 3, 4, 5, 6}.Iter()
//	iter.Skip(3).Collect().Print()
//
// Output: [4, 5, 6]
//
// The resulting iterator will start after skipping the specified number of elements.
func (seq SeqSlice[V]) Skip(n uint) SeqSlice[V] {
	return SeqSlice[V](iter.Skip(iter.Seq[V](seq), int(n)))
}

// StepBy creates a new iterator that iterates over every N-th element of the original iterator.
// This function is useful when you want to skip a specific number of elements between each iteration.
//
// Parameters:
// - n uint: The step size, indicating how many elements to skip between each iteration.
//
// Returns:
// - SeqSlice[V]: A new iterator that produces elements from the original iterator with a step size of N.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
//	iter := slice.Iter().StepBy(3)
//	result := iter.Collect()
//	result.Print()
//
// Output: [1 4 7 10]
//
// The resulting iterator will produce elements from the original iterator with a step size of N.
func (seq SeqSlice[V]) StepBy(n uint) SeqSlice[V] {
	return SeqSlice[V](iter.StepBy(iter.Seq[V](seq), int(n)))
}

// SortBy applies a custom sorting function to the elements in the iterator
// and returns a new iterator containing the sorted elements.
//
// The sorting function 'fn' should take two arguments, 'a' and 'b' of type V,
// and return true if 'a' should be ordered before 'b', and false otherwise.
//
// Example:
//
//	g.SliceOf("a", "c", "b").
//		Iter().
//		SortBy(func(a, b string) cmp.Ordering { return b.Cmp(a) }).
//		Collect().
//		Print()
//
// Output: Slice[c, b, a]
//
// The returned iterator is of type SeqSlice[V], which implements the iterator
// interface for further iteration over the sorted elements.
func (seq SeqSlice[V]) SortBy(fn func(a, b V) cmp.Ordering) SeqSlice[V] {
	return SeqSlice[V](iter.SortBy(iter.Seq[V](seq), func(a, b V) bool { return fn(a, b) == cmp.Less }))
}

// Take returns a new iterator with the first n elements.
// The function creates a new iterator containing the first n elements from the original iterator.
func (seq SeqSlice[V]) Take(n uint) SeqSlice[V] {
	return SeqSlice[V](iter.Take(iter.Seq[V](seq), int(n)))
}

// First returns the first element from the sequence.
func (seq SeqSlice[V]) First() Option[V] {
	return OptionOf(iter.First(iter.Seq[V](seq)))
}

// Last returns the last element from the sequence.
func (seq SeqSlice[V]) Last() Option[V] {
	return OptionOf(iter.Last(iter.Seq[V](seq)))
}

// Nth returns the nth element (0-indexed) in the sequence.
func (seq SeqSlice[V]) Nth(n Int) Option[V] {
	return OptionOf(iter.Nth(iter.Seq[V](seq), int(n)))
}

// ToChan converts the iterator into a channel, optionally with context(s).
//
// The function converts the elements of the iterator into a channel for streaming purposes.
// Optionally, it accepts context(s) to handle cancellation or timeout scenarios.
//
// Params:
//
// - ctxs (context.Context): Optional context(s) to control the channel behavior (e.g., cancellation).
//
// Returns:
//
// - chan V: A channel containing the elements from the iterator.
//
// Example usage:
//
//	iter := g.Slice[int]{1, 2, 3}.Iter()
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel() // Ensure cancellation to avoid goroutine leaks.
//	ch := iter.ToChan(ctx)
//	for val := range ch {
//	    fmt.Println(val)
//	}
//
// The resulting channel allows streaming elements from the iterator with optional context handling.
func (seq SeqSlice[V]) ToChan(ctxs ...context.Context) chan V {
	ctx := context.Background()
	if len(ctxs) > 0 {
		ctx = ctxs[0]
	}

	return iter.ToChan(iter.Seq[V](seq), ctx)
}

// Unique returns an iterator with only unique elements.
//
// The function returns an iterator containing only the unique elements from the original iterator.
//
// Returns:
//
// - SeqSlice[V]: An iterator containing unique elements from the original iterator.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 2, 4, 5, 3}
//	unique := slice.Iter().Unique().Collect()
//	unique.Print()
//
// Output: [1, 2, 3, 4, 5].
//
// The resulting iterator will contain only unique elements from the original iterator.
func (seq SeqSlice[V]) Unique() SeqSlice[V] {
	return SeqSlice[V](iter.Unique(iter.Seq[V](seq)))
}

// Zip combines elements from the current sequence and another sequence into pairs,
// creating an ordered map with identical keys and values of type V.
func (seq SeqSlice[V]) Zip(two SeqSlice[V]) SeqMapOrd[any, any] {
	return func(yield func(any, any) bool) {
		zipSeq := iter.Zip(iter.Seq[V](seq), iter.Seq[V](two))
		zipSeq(func(a, b V) bool {
			return yield(a, b)
		})
	}
}

// Scan accumulates values of the iterator using a function, yielding all intermediate states.
//
// The function takes an initial accumulator value and a function that combines the accumulator
// with each element. It yields the initial value followed by each accumulated state.
//
// Params:
//
// - init (V): The initial accumulator value.
// - fn (func(acc V, val V) V): The function that combines the accumulator with each element.
//
// Returns:
//
// - SeqSlice[V]: A sequence of all intermediate accumulator states.
//
// Example usage:
//
//	numbers := g.Slice[int]{1, 2, 3, 4}.Iter()
//	sums := numbers.Scan(0, func(acc, val int) int {
//		return acc + val
//	})
//	// sums will yield: 0, 1, 3, 6, 10
//
//	words := g.Slice[string]{"a", "b", "c"}.Iter()
//	concatenated := words.Scan("", func(acc, val string) string {
//		return acc + val
//	})
//	// concatenated will yield: "", "a", "ab", "abc"
func (seq SeqSlice[V]) Scan(init V, fn func(acc, val V) V) SeqSlice[V] {
	return func(yield func(V) bool) {
		if !yield(init) {
			return
		}
		iter.Scan(iter.Seq[V](seq), init, fn)(yield)
	}
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
//	iter := g.Slice[int]{1, 2, 3, 4, 5}.Iter()
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
func (seq SeqSlice[V]) Find(fn func(v V) bool) Option[V] {
	return OptionOf(iter.Find(iter.Seq[V](seq), fn))
}

// Windows returns an iterator that yields sliding windows of elements of the specified size.
//
// The function creates a new iterator that yields windows of elements from the original iterator,
// where each window is a slice containing elements of the specified size and moves one element at a time.
//
// Params:
//
// - n (int): The size of each window.
//
// Returns:
//
// - SeqSlices[V]: An iterator yielding sliding windows of elements of the specified size.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5, 6}
//	windows := slice.Iter().Windows(3).Collect()
//
// Output: [Slice[1, 2, 3] Slice[2, 3, 4] Slice[3, 4, 5] Slice[4, 5, 6]]
//
// The resulting iterator will yield sliding windows of elements, each containing the specified number of elements.
func (seq SeqSlice[V]) Windows(n Int) SeqSlices[V] {
	return SeqSlices[V](iter.Windows(iter.Seq[V](seq), int(n)))
}

// Context allows the iteration to be controlled with a context.Context.
func (seq SeqSlice[V]) Context(ctx context.Context) SeqSlice[V] {
	return SeqSlice[V](iter.Context(iter.Seq[V](seq), ctx))
}

// MaxBy returns the maximum element in the sequence using the provided comparison function.
func (seq SeqSlice[V]) MaxBy(fn func(V, V) cmp.Ordering) Option[V] {
	return OptionOf(iter.MaxBy(iter.Seq[V](seq), func(a, b V) bool { return fn(a, b) == cmp.Less }))
}

// Min returns the minimum element in the sequence using the provided comparison function.
func (seq SeqSlice[V]) MinBy(fn func(V, V) cmp.Ordering) Option[V] {
	return OptionOf(iter.MinBy(iter.Seq[V](seq), func(a, b V) bool { return fn(a, b) == cmp.Less }))
}

// Next extracts the next element from the iterator and advances it.
//
// This method consumes the next element from the iterator and returns it wrapped in an Option.
// The iterator itself is modified to point to the remaining elements.
// This is similar to calling Pull() but more convenient for single-element extraction.
//
// Returns:
// - Option[V]: Some(value) if an element exists, None if the iterator is exhausted.
func (seq *SeqSlice[V]) Next() Option[V] {
	if value, remaining, ok := iter.Next(iter.Seq[V](*seq)); ok {
		*seq = SeqSlice[V](remaining)
		return Some(value)
	}

	return None[V]()
}

// FromChan converts a channel into an iterator.
//
// This function takes a channel as input and converts its elements into an iterator,
// allowing seamless integration of channels into iterator-based processing pipelines.
// It continuously reads from the channel until it's closed,
// yielding each element to the provided yield function.
//
// Parameters:
// - ch (<-chan V): The input channel to convert into an iterator.
//
// Returns:
// - SeqSlice[V]: An iterator that yields elements from the channel.
//
// Example usage:
//
//	ch := make(chan int)
//	go func() {
//		defer close(ch)
//		for i := 1; i <= 5; i++ {
//			ch <- i
//		}
//	}()
//
//	// Convert the channel into an iterator and apply filtering and mapping operations.
//	g.FromChan(ch).
//		Filter(func(i int) bool { return i%2 == 0 }). // Filter even numbers.
//		Map(func(i int) int { return i * 2 }).        // Double each element.
//		Collect().                                    // Collect the results into a slice.
//		Print()                                       // Print the collected results.
//
// Output: Slice[4, 8]
//
// The resulting iterator will yield elements from the provided channel, filtering out odd numbers,
// doubling each even number, and finally collecting the results into a slice.
func FromChan[V any](ch <-chan V) SeqSlice[V] {
	return SeqSlice[V](iter.FromChan(ch))
}
