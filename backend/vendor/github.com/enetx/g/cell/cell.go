package cell

import (
	"sync"
	"unsafe"
)

// Cell is a thread-safe wrapper around a value T.
// It provides safe concurrent access through a read-write mutex.
type Cell[T any] struct {
	mu  sync.RWMutex
	val T
}

// New creates a new Cell with the provided value.
//
// Example:
//
//	c := cell.New(42)
//	config := cell.New(Config{Port: 8080, Debug: false})
func New[T any](val T) *Cell[T] {
	return &Cell[T]{val: val}
}

// Get returns the current value stored in the Cell.
func (c *Cell[T]) Get() T {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.val
}

// Set replaces the current value with the given value.
func (c *Cell[T]) Set(value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.val = value
}

// Replace atomically replaces the current value with the new one
// and returns the previous value.
func (c *Cell[T]) Replace(new T) T {
	c.mu.Lock()
	defer c.mu.Unlock()

	old := c.val
	c.val = new

	return old
}

// Swap swaps the values of two cells.
func (c *Cell[T]) Swap(other *Cell[T]) {
	if c == other {
		return
	}

	first, second := c, other
	if uintptr(unsafe.Pointer(c)) > uintptr(unsafe.Pointer(other)) {
		first, second = other, c
	}

	first.mu.Lock()
	defer first.mu.Unlock()

	second.mu.Lock()
	defer second.mu.Unlock()

	c.val, other.val = other.val, c.val
}

// Update atomically updates the value using the provided function.
// The function receives the current value and should return the new value.
// This operation is atomic and thread-safe.
func (c *Cell[T]) Update(fn func(T) T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.val = fn(c.val)
}
