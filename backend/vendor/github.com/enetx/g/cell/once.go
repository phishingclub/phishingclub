package cell

import (
	"errors"
	"sync"

	. "github.com/enetx/g"
)

// OnceCell is a thread-safe cell which can be set exactly once.
// After being set, it provides immutable access to the stored value.
// This is equivalent to Rust's OnceCell.
type OnceCell[T any] struct {
	cell *Cell[Option[T]]
	once sync.Once
	set  bool
}

// NewOnce creates a new empty OnceCell.
//
// Example:
//
//	cell := cell.NewOnce[int]()
//	result := cell.Set(42)
//	if result.IsOk() {
//	    println("Value set successfully")
//	}
//	value := cell.Get()
//	if value.IsSome() {
//	    println("Value:", value.Some())
//	}
func NewOnce[T any]() *OnceCell[T] {
	return &OnceCell[T]{
		cell: New(None[T]()),
		set:  false,
	}
}

// Set attempts to store a value in the cell.
// Returns Ok(()) if the value was stored, Err if the cell was already set.
// This operation is thread-safe and will succeed for exactly one caller.
//
// Example:
//
//	cell := cell.NewOnce[string]()
//	result := cell.Set("hello")  // Returns Ok(())
//	result2 := cell.Set("world") // Returns Err("value already set")
func (o *OnceCell[T]) Set(value T) Result[Unit] {
	success := false

	o.once.Do(func() {
		o.cell.Set(Some(value))
		o.set = true
		success = true
	})

	if success {
		return Ok(Unit{})
	}

	return Err[Unit](errors.New("value already set"))
}

// Get returns Some(value) if the cell has been set, None otherwise.
// This method never blocks and is very fast after the cell has been set.
//
// Example:
//
//	cell := cell.NewOnce[int]()
//	val := cell.Get()
//	if val.IsNone() {
//	    println("Cell is empty")
//	}
//	cell.Set(42)
//	val = cell.Get()
//	println("Value:", val.Some()) // Prints: Value: 42
func (o *OnceCell[T]) Get() Option[T] {
	return o.cell.Get()
}

// GetOrInit returns the value if the cell has been set, or sets and returns
// the result of calling the init function. The init function is guaranteed
// to be called at most once.
//
// Example:
//
//	cell := cell.NewOnce[string]()
//	value := cell.GetOrInit(func() string {
//	    return "initialized"
//	})
//	println(value) // Prints: initialized
//
//	value2 := cell.GetOrInit(func() string {
//	    return "this won't be called"
//	})
//	println(value2) // Prints: initialized
func (o *OnceCell[T]) GetOrInit(init func() T) T {
	if current := o.cell.Get(); current.IsSome() {
		return current.Some()
	}

	o.once.Do(func() {
		if !o.set {
			value := init()
			o.cell.Set(Some(value))
			o.set = true
		}
	})

	return o.cell.Get().Some()
}

// Take removes and returns the value from the cell, if it has been set.
// After calling this method, the cell becomes empty.
//
// Example:
//
//	cell := cell.NewOnce[int]()
//	cell.Set(42)
//	value := cell.Take()
//	println(value.Some()) // 42
//	println(cell.Get().IsNone()) // true
func (o *OnceCell[T]) Take() Option[T] {
	current := o.cell.Get()
	if current.IsSome() {
		o.cell.Set(None[T]())
	}

	return current
}
