package g

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/enetx/g/cmp"
	"github.com/enetx/g/f"
	"github.com/enetx/g/rand"
	"github.com/enetx/iter"
)

// NewSlice creates a new Slice of the given generic type T with the specified length and
// capacity.
// The size variadic parameter can have zero, one, or two integer values.
// If no values are provided, an empty Slice with a length and capacity of 0 is returned.
// If one value is provided, it sets both the length and capacity of the Slice.
// If two values are provided, the first value sets the length and the second value sets the
// capacity.
//
// Parameters:
//
// - size ...Int: A variadic parameter specifying the length and/or capacity of the Slice
//
// Returns:
//
// - Slice[T]: A new Slice of the specified generic type T with the given length and capacity
//
// Example usage:
//
//	s1 := g.NewSlice[int]()        // Creates an empty Slice of type int
//	s2 := g.NewSlice[int](5)       // Creates an Slice with length and capacity of 5
//	s3 := g.NewSlice[int](3, 10)   // Creates an Slice with length of 3 and capacity of 10
func NewSlice[T any](size ...Int) Slice[T] {
	var (
		length   Int
		capacity Int
	)

	switch {
	case len(size) > 1:
		length, capacity = size[0], size[1]
	case len(size) == 1:
		length, capacity = size[0], size[0]
	}

	return make(Slice[T], length, capacity)
}

// TransformSlice applies the given function to each element of a Slice and returns a new Slice
// containing the transformed values.
//
// Parameters:
//
// - sl: The input Slice.
//
// - fn: The function to apply to each element of the input Slice.
//
// Returns:
//
// A new Slice containing the results of applying the function to each element of the input Slice.
func TransformSlice[T, U any](sl Slice[T], fn func(T) U) Slice[U] {
	if len(sl) == 0 {
		return NewSlice[U]()
	}

	result := make(Slice[U], len(sl))
	for i, v := range sl {
		result[i] = fn(v)
	}

	return result
}

// SliceOf creates a new generic slice containing the provided elements.
func SliceOf[T any](slice ...T) Slice[T] { return slice }

// ToHeap converts the slice to a min/max heap with the specified comparison function.
//
// The comparison function should return:
//   - cmp.Less if a < b (for min heap)
//   - cmp.Greater if a > b (for max heap)
//   - cmp.Equal if a == b
//
// Example usage:
//
//	slice := g.SliceOf(5, 2, 8, 1, 9)
//
//	minHeap := slice.ToHeap(cmp.Cmp[int])	// Min heap: Pop() returns smallest
//	maxHeap := slice.ToHeap(func(a, b int) cmp.Ordering {
//		return cmp.Cmp(b, a)
//	})	// Max heap: Pop() returns largest
//
// Time complexity: O(n)
// Space complexity: O(n) - creates a copy of the slice
func (sl Slice[T]) ToHeap(compareFn func(T, T) cmp.Ordering) *Heap[T] {
	if compareFn == nil {
		panic("compareFn cannot be nil")
	}

	h := &Heap[T]{
		data: make(Slice[T], len(sl)),
		cmp:  compareFn,
	}

	copy(h.data, sl)
	h.heapify()

	return h
}

// Transform applies a transformation function to the Slice and returns the result.
func (sl Slice[T]) Transform(fn func(Slice[T]) Slice[T]) Slice[T] { return fn(sl) }

// Iter returns an iterator (SeqSlice[T]) for the Slice, allowing for sequential iteration
// over its elements. It is commonly used in combination with higher-order functions,
// such as 'ForEach', to perform operations on each element of the Slice.
//
// Returns:
//
// A SeqSlice[T], which can be used for sequential iteration over the elements of the Slice.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	iterator := slice.Iter()
//	iterator.ForEach(func(element int) {
//		// Perform some operation on each element
//		fmt.Println(element)
//	})
//
// The 'Iter' method provides a convenient way to traverse the elements of a Slice
// in a functional style, enabling operations like mapping or filtering.
func (sl Slice[T]) Iter() SeqSlice[T] { return SeqSlice[T](iter.FromSlice(sl)) }

// IterReverse returns an iterator (SeqSlice[T]) for the Slice that allows for sequential iteration
// over its elements in reverse order. This method is useful when you need to traverse the elements
// from the end to the beginning.
//
// Returns:
//
// A SeqSlice[T], which can be used for sequential iteration over the elements of the Slice in reverse order.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	iterator := slice.IterReverse()
//	iterator.ForEach(func(element int) {
//		// Perform some operation on each element in reverse order
//		fmt.Println(element)
//	})
//
// The 'IterReverse' method enhances the functionality of the Slice by providing an alternative
// way to iterate through its elements, enhancing flexibility in how data within a Slice is accessed and manipulated.
func (sl Slice[T]) IterReverse() SeqSlice[T] { return SeqSlice[T](iter.FromSliceReverse(sl)) }

// AsAny converts each element of the slice to the 'any' type.
// It returns a new slice containing the elements as 'any' g.Slice[any].
//
// Note: AsAny is useful when you want to work with a slice of a specific type as a slice of 'any'.
// It can be particularly handy in conjunction with Flatten to work with nested slices of different types.
func (sl Slice[T]) AsAny() Slice[any] {
	if sl.Empty() {
		return NewSlice[any]()
	}

	result := make(Slice[any], len(sl))
	for i, v := range sl {
		result[i] = any(v)
	}

	return result
}

// Fill fills the slice with the specified value.
// This function is useful when you want to create an Slice with all elements having the same
// value.
// This method modifies the original slice in place.
//
// Parameters:
//
// - val T: The value to fill the Slice with.
//
// Returns:
//
// - Slice[T]: A reference to the original Slice filled with the specified value.
//
// Example usage:
//
//	slice := g.Slice[int]{0, 0, 0}
//	slice.Fill(5)
//
// The modified slice will now contain: 5, 5, 5.
func (sl Slice[T]) Fill(val T) {
	if len(sl) == 0 {
		return
	}

	if len(sl) > 32 {
		sl[0] = val
		for i := 1; i < len(sl); i <<= 1 {
			copy(sl[i:], sl[:i])
		}
	} else {
		for i := range sl {
			sl[i] = val
		}
	}
}

// Index returns the index of the first occurrence of the specified value in the slice, or -1 if
// not found.
func (sl Slice[T]) Index(val T) Int {
	var zero T
	if f.IsComparable(zero) {
		return sl.IndexBy(func(v T) bool { return f.Eq[any](v)(val) })
	}

	return sl.IndexBy(f.Eqd(val))
}

// IndexBy returns the index of the first element in the slice
// satisfying the custom comparison function provided by the user.
// It iterates through the slice and applies the comparison function to each element and the target value.
// If the comparison function returns true for any pair of elements, it returns the index of that element.
// If no such element is found, it returns -1.
func (sl Slice[T]) IndexBy(fn func(t T) bool) Int { return Int(slices.IndexFunc(sl, fn)) }

// RandomSample returns a new slice containing a random sample of elements from the original slice.
// The sampling is done without replacement, meaning that each element can only appear once in the result.
//
// Parameters:
//
// - sequence int: The number of unique elements to include in the random sample.
//
// Returns:
//
// - Slice[T]: A new Slice containing the random sample of unique elements.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5, 6, 7, 8, 9}
//	sample := slice.RandomSample(3)
//
// The resulting sample will contain 3 unique elements randomly selected from the original slice.
func (sl Slice[T]) RandomSample(sequence Int) Slice[T] {
	if sequence >= sl.Len() {
		return sl.Clone()
	}

	if sequence <= 0 {
		return Slice[T]{}
	}

	n := sl.Len()

	if Float(sequence) < Float(n)*0.25 {
		result := make(Slice[T], sequence)
		swapped := make(map[Int]Int, sequence)

		for i := range sequence {
			j := i + rand.N(n-i)

			vi, foundI := swapped[i]
			if !foundI {
				vi = i
			}

			vj, foundJ := swapped[j]
			if !foundJ {
				vj = j
			}

			swapped[i] = vj
			if i != j {
				swapped[j] = vi
			}

			result[i] = sl[vj]
		}
		return result
	}

	result := sl.Clone()
	result.Shuffle()

	return result[:sequence]
}

// RandomRange returns a new slice containing a random sample of elements from a subrange of the original slice.
// The sampling is done without replacement, meaning that each element can only appear once in the result.
func (sl Slice[T]) RandomRange(from, to Int) Slice[T] {
	if from < 0 {
		from = 0
	}

	if to < 0 || to > sl.Len() {
		to = sl.Len()
	}

	if from > to {
		from = to
	}

	return sl.RandomSample(from.RandomRange(to))
}

// Insert inserts values at the specified index in the slice and modifies the original
// slice.
//
// Parameters:
//
// - i Int: The index at which to insert the new values.
//
// - values ...T: A variadic list of values to insert at the specified index.
//
// Example usage:
//
//	slice := g.Slice[string]{"a", "b", "c", "d"}
//	slice.Insert(2, "e", "f")
//
// The resulting slice will be: ["a", "b", "e", "f", "c", "d"].
func (sl *Slice[T]) Insert(i Int, values ...T) {
	if sl.Empty() {
		if i != 0 {
			panic(Errorf("runtime error: slice bounds out of range [{}] with length 0", i))
		}

		sl.Push(values...)
		return
	}

	sl.Replace(i, i, values...)
}

// Replace replaces the elements of sl[i:j] with the given values,
// and modifies the original slice in place. Replace panics if sl[i:j]
// is not a valid slice of sl.
//
// Parameters:
//
// - i int: The starting index of the slice to be replaced.
//
// - j int: The ending index of the slice to be replaced.
//
// - values ...T: A variadic list of values to replace the existing slice.
//
// Example usage:
//
//	slice := g.Slice[string]{"a", "b", "c", "d"}
//	slice.Replace(1, 3, "e", "f")
//
// After the Replace operation, the resulting slice will be: ["a", "e", "f", "d"].
func (sl *Slice[T]) Replace(i, j Int, values ...T) {
	ii := sl.bound(i)
	jj := sl.bound(j)

	if ii.IsErr() {
		panic(ii.err)
	}
	if jj.IsErr() {
		panic(jj.err)
	}

	i, j = ii.v, jj.v

	if i > j {
		*sl = (*sl)[:0]
		return
	}

	oldLen := sl.Len()
	removedCount := j - i
	addedCount := Int(len(values))
	newLen := oldLen - removedCount + addedCount

	if i == j {
		if addedCount == 0 {
			return
		}

		if newLen > sl.Cap() {
			newSlice := make(Slice[T], newLen)
			copy(newSlice[:i], (*sl)[:i])
			copy(newSlice[i:i+addedCount], values)
			copy(newSlice[i+addedCount:], (*sl)[i:])
			*sl = newSlice
		} else {
			*sl = (*sl)[:newLen]
			copy((*sl)[i+addedCount:], (*sl)[i:oldLen])
			copy((*sl)[i:], values)
		}
		return
	}

	if newLen > sl.Cap() {
		newSlice := make(Slice[T], newLen)
		copy(newSlice[:i], (*sl)[:i])
		copy(newSlice[i:i+addedCount], values)
		copy(newSlice[i+addedCount:], (*sl)[j:])
		*sl = newSlice
	} else {
		if newLen != oldLen {
			*sl = (*sl)[:newLen]
		}

		if addedCount != removedCount {
			copy((*sl)[i+addedCount:], (*sl)[j:oldLen])
		}

		copy((*sl)[i:], values)
	}
}

// Get returns the element at the given index, handling negative indices as counting from the end
// of the slice.
func (sl Slice[T]) Get(index Int) Option[T] {
	i := sl.bound(index)
	if i.IsErr() {
		return None[T]()
	}

	return Some(sl[i.v])
}

// Shuffle shuffles the elements in the slice randomly.
// This method modifies the original slice in place.
//
// The function uses the crypto/rand package to generate random indices.
//
// Example usage:
//
// slice := g.Slice[int]{1, 2, 3, 4, 5}
// slice.Shuffle()
// fmt.Println(slice)
//
// Output: A randomly shuffled version of the original slice, e.g., [4 1 5 2 3].
func (sl Slice[T]) Shuffle() {
	for i := sl.Len() - 1; i > 0; i-- {
		j := rand.N(i + 1)
		sl.swap(i, j)
	}
}

// Reverse reverses the order of the elements in the slice.
// This method modifies the original slice in place.
//
// Returns:
//
// - Slice[T]: The modified slice with the elements reversed.
//
// Example usage:
//
// slice := g.Slice[int]{1, 2, 3, 4, 5}
// slice.Reverse()
// fmt.Println(slice)
//
// Output: [5 4 3 2 1].
func (sl Slice[T]) Reverse() { slices.Reverse(sl) }

// SortBy sorts the elements in the slice using the provided comparison function.
// It modifies the original slice in place. It requires the elements to be of a type
// that is comparable.
//
// The function takes a custom comparison function as an argument and sorts the elements
// of the slice using the provided logic. The comparison function should return true if
// the element at index i should come before the element at index j, and false otherwise.
//
// Parameters:
//
// - f func(a, b T) cmp.Ordered: A comparison function that takes two indices i and j and returns a bool.
//
// Example usage:
//
// sl := NewSlice[int](1, 5, 3, 2, 4)
// sl.SortBy(func(a, b int) cmp.Ordering { return cmp.Cmp(a, b) }) // sorts in ascending order.
func (sl Slice[T]) SortBy(fn func(a, b T) cmp.Ordering) {
	slices.SortFunc(sl, func(a, b T) int { return int(fn(a, b)) })
}

// IsSortedBy checks if the slice is sorted according to the provided comparison function.
//
// The function takes a custom comparison function as an argument and checks if the elements
// are sorted according to the provided logic.
//
// Parameters:
//
// - fn func(a, b T) cmp.Ordering: A comparison function that defines the sort order.
//
// Returns:
//
// - bool: true if the slice is sorted according to the comparison function, false otherwise.
//
// Example usage:
//
//	sl := g.SliceOf(1, 2, 3, 4, 5)
//	sorted := sl.IsSortedBy(func(a, b int) cmp.Ordering { return cmp.Cmp(a, b) }) // returns true
func (sl Slice[T]) IsSortedBy(fn func(a, b T) cmp.Ordering) bool {
	if len(sl) <= 1 {
		return true
	}

	for i := 1; i < len(sl); i++ {
		if fn(sl[i-1], sl[i]).IsGt() {
			return false
		}
	}

	return true
}

// ToStringSlice converts the Slice into a slice of strings.
func (sl Slice[T]) ToStringSlice() []string {
	if len(sl) == 0 {
		return nil
	}

	result := make([]string, len(sl))

	for i, v := range sl {
		switch val := any(v).(type) {
		case String:
			result[i] = val.Std()
		case Int:
			result[i] = strconv.FormatInt(int64(val), 10)
		case Float:
			result[i] = strconv.FormatFloat(float64(val), 'g', -1, 64)
		case Bytes:
			result[i] = string(val)
		case string:
			result[i] = val
		case int:
			result[i] = strconv.Itoa(val)
		case int8:
			result[i] = strconv.FormatInt(int64(val), 10)
		case int16:
			result[i] = strconv.FormatInt(int64(val), 10)
		case int32:
			result[i] = strconv.FormatInt(int64(val), 10)
		case int64:
			result[i] = strconv.FormatInt(val, 10)
		case uint:
			result[i] = strconv.FormatUint(uint64(val), 10)
		case uint8:
			result[i] = strconv.FormatUint(uint64(val), 10)
		case uint16:
			result[i] = strconv.FormatUint(uint64(val), 10)
		case uint32:
			result[i] = strconv.FormatUint(uint64(val), 10)
		case uint64:
			result[i] = strconv.FormatUint(val, 10)
		case float32:
			result[i] = strconv.FormatFloat(float64(val), 'g', -1, 32)
		case float64:
			result[i] = strconv.FormatFloat(val, 'g', -1, 64)
		case bool:
			result[i] = strconv.FormatBool(val)
		default:
			if stringer, ok := any(v).(fmt.Stringer); ok {
				result[i] = stringer.String()
			} else {
				result[i] = fmt.Sprint(v)
			}
		}
	}

	return result
}

// Join joins the elements in the slice into a single String, separated by the provided separator (if any).
func (sl Slice[T]) Join(sep ...T) String {
	if sl.Empty() {
		return ""
	}

	if s, ok := any(sl).(Slice[Bytes]); ok {
		var separator Bytes
		if len(sep) != 0 {
			separator, _ = any(sep[0]).(Bytes)
		}

		return String(bytes.Join(TransformSlice(s, func(b Bytes) []byte { return b }), separator))
	}

	if s, ok := any(sl).(Slice[String]); ok {
		var separator string
		if len(sep) != 0 {
			if sepStr, ok := any(sep[0]).(String); ok {
				separator = sepStr.Std()
			} else {
				separator = fmt.Sprint(sep[0])
			}
		}

		strs := make([]string, len(s))
		for i, str := range s {
			strs[i] = str.Std()
		}

		return String(strings.Join(strs, separator))
	}

	var separator string
	if len(sep) != 0 {
		separator = fmt.Sprint(sep[0])
	}

	return String(strings.Join(sl.ToStringSlice(), separator))
}

// SubSlice returns a new slice containing elements from the current slice between the specified start
// and end indices, with an optional step parameter to define the increment between elements.
// The function checks if the start and end indices are within the bounds of the original slice.
// If the end index is negative, it represents the position from the end of the slice.
// If the start index is negative, it represents the position from the end of the slice counted
// from the start index.
//
// Parameters:
//
// - start (Int): The start index of the range.
//
// - end (Int): The end index of the range.
//
// - step (Int, optional): The increment between elements. Defaults to 1 if not provided.
// If negative, the slice is traversed in reverse order.
//
// Returns:
//
// - Slice[T]: A new slice containing elements from the current slice between the start and end
// indices, with the specified step.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5, 6, 7, 8, 9}
//	subSlice := slice.SubSlice(1, 7, 2) // Extracts elements 2, 4, 6
//	fmt.Println(subSlice)
//
// Output: [2 4 6].
func (sl Slice[T]) SubSlice(start, end Int, step ...Int) Slice[T] {
	if sl.Empty() {
		return sl
	}

	_step := Slice[Int](step).Get(0).UnwrapOr(1)

	ii := sl.bound(start, struct{}{})
	jj := sl.bound(end, struct{}{})

	if ii.IsErr() {
		panic(ii.err)
	}

	if jj.IsErr() {
		panic(jj.err)
	}

	start, end = ii.v, jj.v

	if _step == 1 {
		if start >= end {
			return NewSlice[T]()
		}

		return slices.Clone(sl[start:end])
	}

	if (start >= end && _step > 0) || (start <= end && _step < 0) || _step == 0 {
		return NewSlice[T]()
	}

	var resultSize Int
	if _step > 0 {
		resultSize = (end - start + _step - 1) / _step
	} else {
		resultSize = (start - end + (-_step) - 1) / (-_step)
	}

	slice := make(Slice[T], 0, resultSize)

	var loopCondition func(Int) bool
	if _step > 0 {
		loopCondition = func(i Int) bool { return i < end }
	} else {
		loopCondition = func(i Int) bool { return i > end }
	}

	for i := start; loopCondition(i); i += _step {
		slice = append(slice, sl[i])
	}

	return slice
}

// Random returns a random element from the slice.
//
// The function uses the crypto/rand package to generate a random index within the bounds of the
// slice. If the slice is empty, the zero value of type T is returned.
//
// Returns:
//
// - T: A random element from the slice.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	randomElement := slice.Random()
//	fmt.Println(randomElement)
//
// Output: <any random element from the slice>.
func (sl Slice[T]) Random() T {
	if sl.Empty() {
		var zero T
		return zero
	}

	return sl[rand.N(sl.Len())]
}

// Clone returns a copy of the slice.
func (sl Slice[T]) Clone() Slice[T] {
	if sl.Empty() {
		return NewSlice[T]()
	}

	return slices.Clone(sl)
}

// LastIndex returns the last index of the slice.
func (sl Slice[T]) LastIndex() Int {
	if sl.NotEmpty() {
		return sl.Len() - 1
	}

	return 0
}

// Eq returns true if the slice is equal to the provided other slice.
func (sl Slice[T]) Eq(other Slice[T]) bool {
	var zero T
	if f.IsComparable(zero) {
		return sl.EqBy(other, func(x, y T) bool { return f.Eq[any](x)(y) })
	}

	return sl.EqBy(other, func(x, y T) bool { return f.Eqd(x)(y) })
}

// EqBy reports whether two slices are equal using an equality
// function on each pair of elements. If the lengths are different,
// EqBy returns false. Otherwise, the elements are compared in
// increasing index order, and the comparison stops at the first index
// for which eq returns false.
func (sl Slice[T]) EqBy(other Slice[T], fn func(x, y T) bool) bool {
	return slices.EqualFunc(sl, other, fn)
}

// String returns a string representation of the slice.
func (sl Slice[T]) String() string {
	if len(sl) == 0 {
		return "Slice[]"
	}

	var b Builder
	b.WriteString("Slice[")

	for i, v := range sl {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(Format("{}", v))
	}

	b.WriteString("]")

	return b.String().Std()
}

// Append appends the provided elements to the slice and returns the modified slice.
func (sl Slice[T]) Append(elems ...T) Slice[T] { return append(sl, elems...) }

// AppendUnique appends unique elements from the provided arguments to the current slice.
//
// The function iterates over the provided elements and checks if they are already present
// in the slice. If an element is not already present, it is appended to the slice. The
// resulting slice is returned, containing the unique elements from both the original
// slice and the provided elements.
//
// Parameters:
//
// - elems (...T): A variadic list of elements to be appended to the slice.
//
// Returns:
//
// - Slice[T]: A new slice containing the unique elements from both the original slice
// and the provided elements.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	slice = slice.AppendUnique(3, 4, 5, 6, 7)
//	fmt.Println(slice)
//
// Output: [1 2 3 4 5 6 7].
func (sl Slice[T]) AppendUnique(elems ...T) Slice[T] {
	for _, elem := range elems {
		if !sl.Contains(elem) {
			sl = append(sl, elem)
		}
	}

	return sl
}

// Push appends the provided elements to the slice and modifies the original slice.
func (sl *Slice[T]) Push(elems ...T) { *sl = append(*sl, elems...) }

// PushUnique appends unique elements from the provided arguments to the current slice.
//
// The function iterates over the provided elements and checks if they are already present
// in the slice. If an element is not already present, it is appended to the slice.
//
// Parameters:
//
// - elems (...T): A variadic list of elements to be appended to the slice.
//
// Example usage:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	slice.PushUnique(3, 4, 5, 6, 7)
//	fmt.Println(slice)
//
// Output: [1 2 3 4 5 6 7].
func (sl *Slice[T]) PushUnique(elems ...T) {
	for _, elem := range elems {
		if !sl.Contains(elem) {
			sl.Push(elem)
		}
	}
}

// Cap returns the capacity of the Slice.
func (sl Slice[T]) Cap() Int { return Int(cap(sl)) }

// Contains returns true if the slice contains the provided value.
func (sl Slice[T]) Contains(val T) bool { return sl.Index(val) >= 0 }

// ContainsBy returns true if the slice contains an element that satisfies the provided function fn, false otherwise.
func (sl Slice[T]) ContainsBy(fn func(t T) bool) bool { return sl.IndexBy(fn) >= 0 }

// ContainsAny checks if the Slice contains any element from another Slice.
func (sl Slice[T]) ContainsAny(values ...T) bool {
	if sl.Empty() || len(values) == 0 {
		return false
	}

	return slices.ContainsFunc(values, sl.Contains)
}

// ContainsAll checks if the Slice contains all elements from another Slice.
func (sl Slice[T]) ContainsAll(values ...T) bool {
	if sl.Empty() || len(values) == 0 {
		return len(values) == 0
	}

	for _, v := range values {
		if !sl.Contains(v) {
			return false
		}
	}

	return true
}

// Delete removes an element or a range of elements from the Slice in-place.
// It modifies the original Slice by creating two slices: one from the
// beginning of the Slice up to the specified `start` index (exclusive),
// and another from the `end` index (inclusive) to the end of the Slice.
// These two slices are then concatenated to form the modified Slice.
//
// Parameters:
//
//   - start (Int): The starting index of the element or range to be removed.
//   - end (Int, optional): The end index of the range to be removed.
//     If omitted, only the element at the `start` index is removed.
//
// Note:
//
// The function supports negative indices. Negative values are counted from
// the end of the Slice: for example, -1 refers to the last element, -2 to
// the second-to-last, and so on.
func (sl *Slice[T]) Delete(start Int, end ...Int) {
	sl.Replace(start, Slice[Int](end).Get(0).UnwrapOr(start+1))
}

// Empty returns true if the slice is empty.
func (sl Slice[T]) Empty() bool { return len(sl) == 0 }

// Last returns the last element of the slice.
func (sl Slice[T]) Last() Option[T] { return sl.Get(-1) }

// Ne returns true if the slice is not equal to the provided other slice.
func (sl Slice[T]) Ne(other Slice[T]) bool { return !sl.Eq(other) }

// NeBy reports whether two slices are not equal using an inequality
// function on each pair of elements. If the lengths are different,
// NeBy returns true. Otherwise, the elements are compared in
// increasing index order, and the comparison stops at the first index
// for which fn returns true.
func (sl Slice[T]) NeBy(other Slice[T], fn func(x, y T) bool) bool { return !sl.EqBy(other, fn) }

// NotEmpty checks if the Slice is not empty.
func (sl Slice[T]) NotEmpty() bool { return !sl.Empty() }

// Pop removes and returns the last element of the slice.
// It mutates the original slice by removing the last element.
// It returns None if the slice is empty.
func (sl *Slice[T]) Pop() Option[T] {
	if sl.Len() == 0 {
		return None[T]()
	}

	last := (*sl)[sl.Len()-1]
	*sl = (*sl)[:sl.Len()-1]

	return Some(last)
}

// Set sets the value at the specified index in the slice and returns the modified slice.
// This method modifies the original slice in place.
//
// Parameters:
//
// - index (Int): The index at which to set the new value.
// - val (T): The new value to be set at the specified index.
//
// Returns:
//
// - Slice[T]: The modified slice with the new value set at the specified index.
//
// Example usage:
//
// slice := g.Slice[int]{1, 2, 3, 4, 5}
// slice.Set(2, 99)
// fmt.Println(slice)
//
// Output: [1 2 99 4 5].
func (sl Slice[T]) Set(index Int, val T) {
	i := sl.bound(index)
	if i.IsErr() {
		panic(i.err)
	}

	sl[i.v] = val
}

// Len returns the length of the slice.
func (sl Slice[T]) Len() Int { return Int(len(sl)) }

// Swap swaps the elements at the specified indices in the slice.
// This method modifies the original slice in place.
//
// Parameters:
//
// - i (Int): The index of the first element to be swapped.
//
// - j (Int): The index of the second element to be swapped.
//
// Returns:
//
// - Slice[T]: The modified slice with the elements at the specified indices swapped.
//
// Example usage:
//
// slice := g.Slice[int]{1, 2, 3, 4, 5}
// slice.Swap(1, 3)
// fmt.Println(slice)
//
// Output: [1 4 3 2 5].
func (sl Slice[T]) Swap(i, j Int) {
	ii := sl.bound(i)
	jj := sl.bound(j)

	if ii.IsErr() {
		panic(ii.err)
	}

	if jj.IsErr() {
		panic(jj.err)
	}

	sl.swap(ii.v, jj.v)
}

func (sl Slice[T]) swap(i, j Int) { sl[i], sl[j] = sl[j], sl[i] }

// Grow increases the slice's capacity, if necessary, to guarantee space for
// another n elements. After Grow(n), at least n elements can be appended
// to the slice without another allocation. If n is negative or too large to
// allocate the memory, Grow panics.
func (sl Slice[T]) Grow(n Int) Slice[T] { return slices.Grow(sl, n.Std()) }

// Clip removes unused capacity from the slice.
func (sl Slice[T]) Clip() Slice[T] { return slices.Clip(sl) }

// Std returns a new slice with the same elements as the Slice[T].
func (sl Slice[T]) Std() []T { return sl }

// Print writes the elements of the Slice to the standard output (console)
// and returns the Slice unchanged.
func (sl Slice[T]) Print() Slice[T] { fmt.Print(sl); return sl }

// Println writes the elements of the Slice to the standard output (console) with a newline
// and returns the Slice unchanged.
func (sl Slice[T]) Println() Slice[T] { fmt.Println(sl); return sl }

// Unpack assigns values of the slice's elements to the variables passed as pointers.
// If the number of variables passed is greater than the length of the slice,
// the function ignores the extra variables.
//
// Parameters:
//
// - vars (...*T): Pointers to variables where the values of the slice's elements will be stored.
//
// Example:
//
//	slice := g.Slice[int]{1, 2, 3, 4, 5}
//	var a, b, c int
//	slice.Unpack(&a, &b, &c)
//	fmt.Println(a, b, c) // Output: 1 2 3
func (sl Slice[T]) Unpack(vars ...*T) {
	n := min(len(sl), len(vars))

	for i := range n {
		if vars[i] != nil {
			*vars[i] = sl[i]
		}
	}
}

// MaxBy returns the maximum value in the slice according to the provided comparison function fn.
// It applies fn pairwise to the elements of the slice until it finds the maximum value.
// It returns the maximum value found.
//
// Example:
//
//	s := Slice[int]{3, 1, 4, 2, 5}
//	maxInt := s.MaxBy(cmp.Cmp)
//	fmt.Println(maxInt) // Output: 5
func (sl Slice[T]) MaxBy(fn func(a, b T) cmp.Ordering) T { return cmp.MaxBy(fn, sl...) }

// MinBy returns the minimum value in the slice according to the provided comparison function fn.
// It applies fn pairwise to the elements of the slice until it finds the minimum value.
// It returns the minimum value found.
//
// Example:
//
//	s := Slice[int]{3, 1, 4, 2, 5}
//	minInt := s.MinBy(cmp.Cmp)
//	fmt.Println(minInt) // Output: 1
func (sl Slice[T]) MinBy(fn func(a, b T) cmp.Ordering) T { return cmp.MinBy(fn, sl...) }

func (sl Slice[T]) bound(i Int, subslice ...struct{}) Result[Int] {
	if sl.Empty() {
		return Err[Int](errors.New("runtime error: slice is empty"))
	}

	ii := i
	if ii < 0 {
		ii += sl.Len()
	}

	var negative Int
	if len(subslice) != 0 {
		negative = -1
	}

	if ii > sl.Len() || ii < negative {
		return Err[Int](Errorf("runtime error: slice bounds out of range [{}] with length {}", i, len(sl)))
	}

	return Ok(ii)
}
