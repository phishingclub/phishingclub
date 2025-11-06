package g

import (
	"fmt"

	"github.com/enetx/g/cmp"
)

// NewHeap creates a new heap with the given comparison function.
// The comparison function should return:
// - cmp.Less if the first argument should have higher priority
// - cmp.Greater if the second argument should have higher priority
// - cmp.Equal if they have equal priority
func NewHeap[T any](compareFn func(T, T) cmp.Ordering) *Heap[T] {
	return &Heap[T]{
		data: make(Slice[T], 0),
		cmp:  compareFn,
	}
}

// Transform applies a transformation function to the Heap and returns the result.
func (h *Heap[T]) Transform(fn func(*Heap[T]) *Heap[T]) *Heap[T] { return fn(h) }

// Iter returns a non-consuming iterator that yields elements in sorted order.
//
// The iterator creates a clone of the heap and yields elements by repeatedly
// calling Pop() on the clone, ensuring the original heap remains unchanged.
// Elements are yielded in the order determined by the heap's comparison function
// (smallest first for min-heap, largest first for max-heap).
//
// Time complexity: O(n log n) for full iteration
// Space complexity: O(n) for the heap clone
//
// Returns:
//
// - SeqSlice[T]: An iterator that yields elements in sorted order
//
// Example usage:
//
//	heap := g.NewHeap(cmp.Cmp[int])
//	heap.Push(10, 5, 15, 1, 8)
//
//	// Iterate without consuming the original heap
//	heap.Iter().ForEach(func(x int) {
//		fmt.Printf("%d ", x) // Output: 1 5 8 10 15
//	})
//
//	fmt.Printf("Heap still has %d elements\n", heap.Len()) // Output: 5
//
//	// Can be used with other iterator methods
//	firstThree := heap.Iter().Take(3).Collect() // [1, 5, 8]
//	evenNumbers := heap.Iter().Filter(func(x int) bool {
//		return x%2 == 0
//	}).Collect() // [8, 10]
func (h *Heap[T]) Iter() SeqHeap[T] {
	return func(yield func(T) bool) {
		clone := h.Clone()
		for !clone.Empty() {
			if !yield(clone.Pop().Some()) {
				return
			}
		}
	}
}

// IntoIter returns a consuming iterator that yields elements in sorted order.
//
// This iterator consumes the original heap by repeatedly calling Pop() until
// the heap is empty. After iteration completes (or is stopped early), the
// original heap will be empty. Elements are yielded in the order determined
// by the heap's comparison function (smallest first for min-heap, largest first for max-heap).
//
// Use this method when you want to consume the heap and don't need the original
// data structure afterwards, or when you want to transfer ownership of the elements.
//
// Time complexity: O(n log n) for full iteration
// Space complexity: O(1) - no additional memory allocation
//
// Returns:
//
// - SeqSlice[T]: An iterator that yields elements in sorted order while consuming the heap
//
// Example usage:
//
//	heap := g.NewHeap(cmp.Cmp[int])
//	heap.Push(10, 5, 15, 1, 8)
//
//	// Consume the heap while iterating
//	result := heap.IntoIter().Collect() // [1, 5, 8, 10, 15]
//
//	fmt.Printf("Heap now has %d elements\n", heap.Len()) // Output: 0
//
//	// Can be stopped early, leaving remaining elements in heap
//	heap2 := g.NewHeap(cmp.Cmp[int])
//	heap2.Push(20, 25, 15, 30)
//
//	heap2.IntoIter().Take(2).ForEach(func(x int) {
//		fmt.Printf("%d ", x) // Output: 15 20
//	})
//	fmt.Printf("Remaining: %d elements\n", heap2.Len()) // Output: 2
func (h *Heap[T]) IntoIter() SeqHeap[T] {
	return func(yield func(T) bool) {
		for !h.Empty() {
			if !yield(h.Pop().Some()) {
				return
			}
		}
	}
}

// Push adds one or more items to the heap.
func (h *Heap[T]) Push(items ...T) {
	for _, item := range items {
		h.data = append(h.data, item)
		h.heapifyUp(len(h.data) - 1)
	}
}

// Pop removes and returns the top element from the heap.
// Returns None if the heap is empty.
func (h *Heap[T]) Pop() Option[T] {
	if len(h.data) == 0 {
		return None[T]()
	}

	top := h.data[0]
	last := len(h.data) - 1
	h.data[0] = h.data[last]
	h.data = h.data[:last]

	if len(h.data) > 0 {
		h.heapifyDown(0)
	}

	return Some(top)
}

// Peek returns the top element without removing it.
// Returns None if the heap is empty.
func (h *Heap[T]) Peek() Option[T] {
	if len(h.data) == 0 {
		return None[T]()
	}

	return Some(h.data[0])
}

// Len returns the number of elements in the heap.
func (h *Heap[T]) Len() Int {
	return h.data.Len()
}

// Empty returns true if the heap contains no elements.
func (h *Heap[T]) Empty() bool {
	return len(h.data) == 0
}

// ToSlice returns a slice containing all elements in the heap.
// The order is not guaranteed to be sorted.
func (h *Heap[T]) ToSlice() Slice[T] {
	result := make(Slice[T], len(h.data))
	copy(result, h.data)

	return result
}

// Clear removes all elements from the heap.
func (h *Heap[T]) Clear() {
	h.data = h.data[:0]
}

// Clone creates a deep copy of the heap.
func (h *Heap[T]) Clone() *Heap[T] {
	return &Heap[T]{
		data: h.data.Clone(),
		cmp:  h.cmp,
	}
}

// heapify transforms the entire data slice into a valid heap.
func (h *Heap[T]) heapify() {
	for i := len(h.data)/2 - 1; i >= 0; i-- {
		h.heapifyDown(i)
	}
}

// heapifyUp maintains heap property by moving element up.
func (h *Heap[T]) heapifyUp(idx int) {
	for idx > 0 {
		parent := (idx - 1) / 2

		if h.cmp(h.data[idx], h.data[parent]) != cmp.Less {
			break
		}

		h.data[idx], h.data[parent] = h.data[parent], h.data[idx]
		idx = parent
	}
}

// heapifyDown maintains heap property by moving element down.
func (h *Heap[T]) heapifyDown(idx int) {
	for {
		smallest := idx
		left := 2*idx + 1
		right := 2*idx + 2

		if left < len(h.data) && h.cmp(h.data[left], h.data[smallest]) == cmp.Less {
			smallest = left
		}

		if right < len(h.data) && h.cmp(h.data[right], h.data[smallest]) == cmp.Less {
			smallest = right
		}

		if smallest == idx {
			break
		}

		h.data[idx], h.data[smallest] = h.data[smallest], h.data[idx]
		idx = smallest
	}
}

// String returns a string representation of the heap.
func (h Heap[T]) String() string {
	if len(h.data) == 0 {
		return "Heap[]"
	}

	var b Builder
	b.WriteString("Heap[")

	for i, v := range h.data {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(Format("{}", v))
	}

	b.WriteString("]")

	return b.String().Std()
}

// Print writes the elements of the Heap to the standard output (console)
// and returns the Heap unchanged.
func (h *Heap[T]) Print() *Heap[T] { fmt.Print(h); return h }

// Println writes the elements of the Heap to the standard output (console) with a newline
// and returns the Heap unchanged.
func (h *Heap[T]) Println() *Heap[T] { fmt.Println(h); return h }
