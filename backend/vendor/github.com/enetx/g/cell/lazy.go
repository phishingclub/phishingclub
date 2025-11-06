package cell

import (
	"sync"

	. "github.com/enetx/g"
)

// LazyCell is a thread-safe, lazy-initialization wrapper around a computation.
// The computation function is executed at most once, on the first call to Force().
// Subsequent calls return the cached result.
// Internally uses Cell for thread-safe operations.
type LazyCell[T any] struct {
	cell *Cell[Option[T]]
	fn   func() T
	once sync.Once
}

// NewLazy creates a new LazyCell wrapper around the given computation function.
//
// The function will not be executed until the first call to Force().
// The function should be idempotent and side-effect free for predictable behavior.
//
// Example:
//
//	expensive := cell.NewLazy(func() int {
//	    time.Sleep(1 * time.Second)
//	    return 42
//	})
//
//	// Function not called yet
//	result := expensive.Force() // Function called here
//	result2 := expensive.Force() // Cached result returned
func NewLazy[T any](fn func() T) *LazyCell[T] {
	return &LazyCell[T]{
		cell: New(None[T]()),
		fn:   fn,
	}
}

// Force executes the computation function (if not already executed) and returns the result.
//
// The function is guaranteed to be called at most once, even in concurrent scenarios.
// All subsequent calls return the same cached value.
//
// This method is thread-safe and can be called from multiple goroutines concurrently.
func (l *LazyCell[T]) Force() T {
	l.once.Do(func() {
		result := l.fn()
		l.cell.Set(Some(result))
	})

	return l.cell.Get().Some()
}

// Get returns Some(value) if the lazy value has been computed, None otherwise.
// This method never triggers the computation - it only returns already computed results.
//
// Example:
//
//	if val := lazy.Get(); val.IsSome() {
//	    fmt.Println("Already computed:", val.Some())
//	} else {
//	    fmt.Println("Not computed yet")
//	}
func (l *LazyCell[T]) Get() Option[T] {
	return l.cell.Get()
}
